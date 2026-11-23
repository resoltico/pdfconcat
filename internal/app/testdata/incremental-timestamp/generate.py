"""Generate the synthetic timestamp reproduction and effective-xref controls."""
from pathlib import Path
import sys
import zlib

pdf = bytearray(b"%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
page = b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << >> /Contents %d 0 R%s >>"
def stream(data):
    return b"<< /Length %d >>\nstream\n%s\nendstream" % (len(data), data)
def append_revision(objects, size, previous=None, freed=()):
    offsets = {}
    for n, body in objects.items():
        offsets[n] = len(pdf)
        pdf.extend(b"%d 0 obj\n%s\nendobj\n" % (n,body))
    xref = len(pdf)
    pdf.extend(b"xref\n")
    if previous is None:
        pdf.extend(b"0 %d\n0000000000 65535 f \n" % size)
    for n, offset in offsets.items():
        if previous is not None:
            pdf.extend(b"%d 1\n" % n)
        pdf.extend(b"%010d 00000 n \n" % offset)
    for i, n in enumerate(freed):
        next_free = freed[i + 1] if i + 1 < len(freed) else 0
        pdf.extend(b"%d 1\n%010d 00001 f \n" % (n, next_free))
    trailer = b"<< /Size %d /Root 1 0 R" % size
    if previous is not None:
        trailer += b" /Prev %d" % previous
    pdf.extend(b"trailer\n%s >>\nstartxref\n%d\n%%%%EOF\n" % (trailer,xref))
    return xref

prev = append_revision({
    1: b"<< /Type /Catalog /Pages 2 0 R >>",
    2: b"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
    3: page % (5,b""), 4: page % (6,b""),
    5: stream(b"0 0 0 rg 50 50 20 20 re f"), 6: stream(b"0 0 1 rg 75 75 30 30 re f"),
},7)
original = bytes(pdf)
timestamp_objects = {
    1: b"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
    4: page % (6,b" /Annots [8 0 R]"),
    7: b"<< /Fields [8 0 R] /SigFlags 3 >>",
    8: b"<< /Type /Annot /Subtype /Widget /FT /Sig /T (Signature1) /F 132 /Rect [0 0 0 0] /P 4 0 R /V 9 0 R >>",
    9: b"<< /Type /DocTimeStamp /Filter /Adobe.PPKLite /SubFilter /ETSI.RFC3161 /ByteRange [0 1111111111 2222222222 3333333333] /Contents <00000000> >>",
}

def fill_byte_range():
    start = pdf.index(b"<00000000>")
    end = start + len(b"<00000000>")
    for placeholder, number in [(b"1111111111",start),(b"2222222222",end),(b"3333333333",len(pdf)-end)]:
        pdf[:] = pdf.replace(placeholder,b"%010d" % number)

timestamp_revision = append_revision(timestamp_objects,10,prev)
fill_byte_range()
if len(sys.argv) != 2:
    raise SystemExit("Usage: python3 generate.py OUTPUT_DIRECTORY")
directory = Path(sys.argv[1])
directory.mkdir(parents=True, exist_ok=True)
(directory / "original.pdf").write_bytes(original)
timestamp = bytes(pdf)
(directory / "incremental-timestamp.pdf").write_bytes(timestamp)

# A later revision changes page artwork and inherits the timestamp revision.
append_revision({5: stream(b"0 1 0 rg 50 50 20 20 re f")}, 10, timestamp_revision)
(directory / "inherited-timestamp.pdf").write_bytes(pdf)

# The effective catalog and page are again unsigned. An overwritten widget and
# a freed timestamp must not be reconstructed from the previous revision.
pdf[:] = timestamp
append_revision({
    1: b"<< /Type /Catalog /Pages 2 0 R >>",
    4: page % (6, b""),
    7: b"null",
    8: b"null",
}, 10, timestamp_revision, freed=(9,))
(directory / "superseded-timestamp.pdf").write_bytes(pdf)


# Model the invisible iText timestamp with a fresh, compressed Form on
# the final revision. Keep the genuinely appearance-free fixture above.
pdf[:] = original
empty_appearance = stream(zlib.compress(b"")).replace(
    b" >>", b" /Type /XObject /Subtype /Form /FormType 1 /BBox [0 0 0 0]"
    b" /Matrix [1 0 0 1 0 0] /Resources << >> /Filter /FlateDecode >>", 1,
)
compressed_objects = timestamp_objects | {
    1: timestamp_objects[1][:-2] + b"/Extensions << /ESIC << /BaseVersion /1.7 /ExtensionLevel 5 >> >> >>",
    8: timestamp_objects[8][:-2] + b"/AP << /N 10 0 R >> >>",
    10: empty_appearance,
}
append_revision(compressed_objects, 11, prev)
fill_byte_range()
(directory / "compressed-empty-appearance.pdf").write_bytes(pdf)

pdf[:] = original
append_revision(compressed_objects | {
    4: page % (6, b" /Annots [8 0 R] /Group << /S /Transparency /CS /DeviceRGB /I true >>"),
}, 11, prev)
fill_byte_range()
(directory / "grouped-empty-appearance.pdf").write_bytes(pdf)
