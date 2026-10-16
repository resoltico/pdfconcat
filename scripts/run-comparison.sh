#!/usr/bin/env bash
# Compare the pinned pdfcpu CLI with a rebuilt executable using fresh small fixtures.
# Usage: scripts/run-comparison.sh PDFCONCAT_EXECUTABLE WORK_BASE
set -euo pipefail
comparison_repo=$(cd "$(dirname "$0")/.." && pwd)
exec python3 -I - "$comparison_repo" "$@" <<'PY'
import json, os, pathlib, re, shutil, subprocess, sys, tempfile

if len(sys.argv) != 4:
    raise SystemExit("usage: scripts/run-comparison.sh PDFCONCAT_EXECUTABLE WORK_BASE")
repo = pathlib.Path(sys.argv[1]).resolve()
pc = pathlib.Path(sys.argv[2]).resolve()
base = pathlib.Path(sys.argv[3]).resolve()
if not pc.is_file():
    raise SystemExit("PDFConcat executable is missing: " + str(pc))
for tool in ("go", "pdfcpu", "qpdf", "pdftotext", "pdfinfo", "pdftohtml"):
    if shutil.which(tool) is None:
        raise SystemExit("missing comparison prerequisite: " + tool)
base.mkdir(parents=True, exist_ok=True)
work = pathlib.Path(tempfile.mkdtemp(prefix="comparison-", dir=base))
sequence = 0

def run(args, expected=0, env=None, cwd=None, no_diagnostics=False):
    global sequence
    sequence += 1
    p = subprocess.run([str(a) for a in args], cwd=cwd or work, env=env,
                       capture_output=True, text=True)
    stem = work / ("command-%02d" % sequence)
    stem.with_suffix(".stdout").write_text(p.stdout, encoding="utf-8")
    stem.with_suffix(".stderr").write_text(p.stderr, encoding="utf-8")
    if p.returncode != expected:
        raise RuntimeError("command exit %s, expected %s: %r\n%s\n%s" %
                           (p.returncode, expected, args, p.stdout, p.stderr))
    if no_diagnostics and p.stderr.strip():
        raise RuntimeError("unexpected reader diagnostics: " + p.stderr)
    return p.stdout

def fresh_pdf(name):
    path = work / name
    if not path.is_file() or not path.read_bytes().startswith(b"%PDF-"):
        raise RuntimeError("expected newly produced PDF: " + str(path))
    return path

def pages(path):
    return int(run(["qpdf", "--show-npages", path]).strip())

def result(args, code=0, diagnostic=None):
    value = json.loads(run([pc] + args, expected=code))
    if value.get("format_version") != 4:
        raise RuntimeError("unexpected result version")
    if diagnostic and diagnostic not in [d["code"] for d in value["diagnostics"]]:
        raise RuntimeError("missing expected refusal " + diagnostic)
    if code == 0 and value.get("command") in ("build", "check") and value.get("status") != "ok":
        raise RuntimeError("non-ok successful exit")
    return value

def targets(path):
    data = json.loads(run(["qpdf", "--json", path]))
    objects = data["qpdf"][1]
    page_index = {p["object"]: i + 1 for i, p in enumerate(data["pages"])}
    def resolve(value):
        for _ in range(30):
            if not isinstance(value, str) or not value.endswith(" R"):
                return value
            wrapper = objects.get("obj:" + value, {})
            value = wrapper.get("value", wrapper.get("stream", {}).get("dict"))
        raise RuntimeError("reference cycle in qpdf comparison data")
    catalog = resolve(objects["trailer"]["value"]["/Root"])
    names = {}
    def collect(node):
        node = resolve(node) or {}
        entries = resolve(node.get("/Names", []))
        for i in range(0, len(entries), 2):
            names[entries[i].removeprefix("u:").removeprefix("b:")] = resolve(entries[i + 1])
        for kid in resolve(node.get("/Kids", [])):
            collect(kid)
    collect(resolve(catalog.get("/Names", {})).get("/Dests", {}))
    legacy = resolve(catalog.get("/Dests", {}))
    def target(value):
        value = resolve(value)
        if isinstance(value, str):
            if value.startswith("/"):
                value = resolve(legacy.get(value, names.get(value[1:])))
            else:
                value = resolve(names.get(value.removeprefix("u:").removeprefix("b:")))
        if isinstance(value, dict):
            value = resolve(value.get("/D"))
        return page_index.get(value[0]) if isinstance(value, list) and value else None
    found = {}
    for number, page in enumerate(data["pages"], 1):
        for annotation in resolve(resolve(page["object"]).get("/Annots", [])):
            annotation = resolve(annotation)
            if annotation.get("/Subtype") != "/Link":
                continue
            destination = annotation.get("/Dest")
            if destination is None:
                destination = resolve(annotation.get("/A", {})).get("/D")
            found.setdefault(number, []).append(target(destination))
    return found

# Versions are checked before fixture generation or long commands.
pin = run(["go", "list", "-m", "-f", "{{.Version}}", "github.com/pdfcpu/pdfcpu"], cwd=repo).strip()
cpu_version = run(["pdfcpu", "version"])
reported_versions = [value.strip() for value in re.findall(
    r"^[ \t]*version:[ \t]*(.*)$", cpu_version, flags=re.MULTILINE)]
if reported_versions != [pin]:
    raise RuntimeError("pdfcpu CLI version must equal module pin %s; reported %r" % (pin, reported_versions))
pc_version = result(["version"])
env = dict(os.environ, PDFCONCAT_COMPARISON_DIR=str(work))
run(["go", "test", "./internal/core", "-run", "^TestComparisonFixtures$", "-count=1"], env=env, cwd=repo)

sources = ["plain-a.pdf", "links.pdf", "form.pdf", "links.pdf", "form.pdf", "named.pdf", "named.pdf", "outlined.pdf", "outlined.pdf"]
run(["pdfcpu", "merge", "-c", "disable", "-b=false", "cpu-merge.pdf"] + sources)
merged = result(["build", "-o", "pc-merge.pdf", "--report", "pc-report.json"] + sources)
if not merged["output"]["published"] or pages(fresh_pdf("cpu-merge.pdf")) != 17 or pages(fresh_pdf("pc-merge.pdf")) != 17:
    raise RuntimeError("ordered-source page count mismatch")
report_path = work / "pc-report.json"
if not report_path.is_file():
    raise RuntimeError("expected newly written report")
report = json.loads(report_path.read_text(encoding="utf-8"))
if len(report["parts"]) != 9 or not report["parts"][4]["fields_renamed"]:
    raise RuntimeError("missing actual form rename records")
pc_targets, cpu_targets = targets(work / "pc-merge.pdf"), targets(work / "cpu-merge.pdf")
for page, expected in {10: 11, 11: 10, 12: 13, 13: 12}.items():
    if pc_targets.get(page) != [expected]:
        raise RuntimeError("PDFConcat named target mismatch: " + str(pc_targets))
if cpu_targets.get(10) != [13] or cpu_targets.get(11) != [12]:
    raise RuntimeError("pdfcpu repeated-names behavior changed; update the comparison")
# Poppler resolves raw destination bytes, unlike decoded JSON alone.
run(["pdftohtml", "-i", "-stdout", work / "pc-merge.pdf"], no_diagnostics=True)

run(["pdfcpu", "merge", "-c", "disable", "-b=false", "cpu-legacy.pdf", "plain-a.pdf", "legacy-dests.pdf", "legacy-dests.pdf"])
result(["build", "-o", "pc-legacy.pdf", "plain-a.pdf", "legacy-dests.pdf", "legacy-dests.pdf"])
legacy_pc, legacy_cpu = targets(fresh_pdf("pc-legacy.pdf")), targets(fresh_pdf("cpu-legacy.pdf"))
if legacy_pc.get(2) != [3] or legacy_pc.get(3) != [2] or legacy_cpu.get(2) != [5]:
    raise RuntimeError("legacy destination observation changed")

text = "Pielikums Ā ļ ķ — Ελληνικά Кириллица"
plan = {"version": 3, "output": "pc-unicode.pdf", "items": [{"source":"plain-a.pdf"}, {"page": {"text": {"value": text, "size": 18}}}]}
(work / "unicode-plan.json").write_text(json.dumps(plan, ensure_ascii=False), encoding="utf-8")
result(["check", "--plan", "unicode-plan.json"])
if (work / "pc-unicode.pdf").exists():
    raise RuntimeError("check wrote a PDF")
result(["build", "--plan", "unicode-plan.json"])
extracted = run(["pdftotext", "-f", "2", "-l", "2", "-raw", fresh_pdf("pc-unicode.pdf"), "-"]).strip("\n\f")
if extracted != text:
    raise RuntimeError("generated Unicode extraction mismatch: " + repr(extracted))
core = {"paper": "Letter", "pages": {"1": {"content": {"text": [{"value": text, "anchor": "center", "font": {"name": "Helvetica", "size": 18}}]}}}}
(work / "core.json").write_text(json.dumps(core, ensure_ascii=False), encoding="utf-8")
run(["pdfcpu", "create", "-c", "disable", "core.json", "cpu-core.pdf"])
core_text = run(["pdftotext", fresh_pdf("cpu-core.pdf"), "-"]).strip()
if "Pielikums" not in core_text or any(word in core_text for word in ("Ā", "Ελληνικά", "Кириллица")):
    raise RuntimeError("core-font observation changed: " + repr(core_text))

# The same font establishes shared Unicode capability, independently of core fonts.
config = work / "pdfcpu-config"
config.mkdir()
run(["pdfcpu", "fonts", "install", "-c", config, repo / "internal/typeset/fontdata/NotoSans-Regular.ttf"])
core["pages"]["1"]["content"]["text"][0]["font"]["name"] = "NotoSans-Regular"
(work / "noto.json").write_text(json.dumps(core, ensure_ascii=False), encoding="utf-8")
run(["pdfcpu", "create", "-c", config, "noto.json", "cpu-noto.pdf"])
noto_text = run(["pdftotext", "-raw", fresh_pdf("cpu-noto.pdf"), "-"]).strip("\n\f")
if noto_text != text:
    raise RuntimeError("installed-font Unicode extraction mismatch: " + repr(noto_text))

# Simple blank-divider parity: counts and every page's dimensions, not inheritance.
run(["pdfcpu", "merge", "-c", "disable", "-b=false", "--divider", "cpu-divider.pdf", "plain-a.pdf", "links.pdf"])
result(["build", "-o", "pc-divider.pdf", "plain-a.pdf", "--blank", "links.pdf"])
for name in ("cpu-divider.pdf", "pc-divider.pdf"):
    path = fresh_pdf(name)
    if pages(path) != 4:
        raise RuntimeError("blank-divider page count mismatch: " + name)
    info = run(["pdfinfo", "-f", "1", "-l", "4", path])
    sizes = re.findall(r"Page\s+\d+ size:\s+([0-9.]+) x ([0-9.]+) pts", info)
    if len(sizes) != 4 or any((float(w), float(h)) != (612, 792) for w, h in sizes):
        raise RuntimeError("blank-divider sheet dimensions mismatch: " + info)

run(["pdfcpu", "merge", "-c", "disable", "-b=false", "cpu-signed.pdf", "plain-a.pdf", "signed.pdf"])
if pages(fresh_pdf("cpu-signed.pdf")) != 2:
    raise RuntimeError("signed-source merge did not produce its declared pages")
result(["check", "plain-a.pdf", "signed.pdf"], code=2, diagnostic="source_signed")
run(["pdfcpu", "merge", "-c", "disable", "-b=false", "cpu-damaged.pdf", "damaged.pdf", "plain-a.pdf"])
result(["check", "damaged.pdf", "plain-a.pdf"], code=2, diagnostic="source_damaged")
run(["pdfcpu", "resize", "-c", "disable", "form:A4P, rotate:off", "fitmarks.pdf", "cpu-fit.pdf"])
result(["build", "--fit-to", "A4", "-o", "pc-fit.pdf", "fitmarks.pdf"])
fresh_pdf("cpu-fit.pdf")
fresh_pdf("pc-fit.pdf")
# The pinned resize leaves local scroll coordinates unchanged; refuse this input.
result(["check", "--fit-to", "A4", "links.pdf"], code=2, diagnostic="fit_destination_unsupported")
result(["build", "--fit-to", "A4", "-o", "pc-fit-refused.pdf", "links.pdf"], code=2, diagnostic="fit_destination_unsupported")
if (work / "pc-fit-refused.pdf").exists():
    raise RuntimeError("refused fitting published a PDF")
summary = {"pdfcpu": reported_versions[0], "pdfconcat": pc_version["version"], "ordered_pages": 17,
           "named_targets_pdfconcat": pc_targets, "named_targets_pdfcpu": cpu_targets,
           "legacy_targets_pdfconcat": legacy_pc, "legacy_targets_pdfcpu": legacy_cpu,
           "unicode": extracted, "installed_font_unicode_pdfcpu": noto_text,
           "blank_divider_pages": 4, "blank_divider_size_pt": [612, 792],
           "core_font_text": core_text,
           "damaged_cpu_pages": pages(fresh_pdf("cpu-damaged.pdf")),
           "short_separator_growth_bytes": (work / "pc-unicode.pdf").stat().st_size - (work / "plain-a.pdf").stat().st_size,
           "workdir": str(work)}
(work / "comparison-result.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps(summary, ensure_ascii=False, indent=2))
PY
