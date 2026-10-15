# Verification closeout — 2026-10-15

The operator requested ending the extended mutation/testing effort and consolidating the repository onto `main`, including known failed or incomplete verification. No further full mutation campaign was started for this closeout. This is a development merge, not a release-readiness or complete-acceptance claim. The source-release workflow still requires its complete exact-source campaigns; no gate, exception policy or branch protection was weakened.

## Evidence and remaining limits

- Committed baseline `9c4645b17ea61751f61f08151a917b6aa0acc502` passed all 35 [native CI jobs](https://github.com/resoltico/pdfconcat/actions/runs/38634493322), with the native event/coverage archives independently verified. Its local full race run passed 4,089 top-level tests and 7,474 subtests across 56 packages, with no named skips.
- Its [43-job campaign](https://github.com/resoltico/pdfconcat/actions/runs/38634601725) finished with 36 successful and seven failed jobs: six mutation partitions and the aggregate. Thirty mutation partitions have complete independently checked reports. The campaign did not pass.
- Windows partitions 5 and 9 suffered real mutant panics while unrelated parallel permission fixtures held deny-delete ACLs. The closing change serializes those fixtures without relaxing native checks or SDK cleanup refusal. Three actual pinned-SDK replays verified named failures, source restoration, settled processes and empty temporary roots; all required serial fixture events preceded each panic. The [PowerShell probe](https://github.com/resoltico/pdfconcat/actions/runs/38649850612) retained those passing Go results but its Python wrapper later failed decoding UTF-8 JSON using the Windows default encoding. It remains an overall failed probe.
- Windows partition 4 encountered an unsigned-underflow mutant requesting a 4 GiB allocation. The closing change computes allocation size in signed `int` under the existing DWORD/MaxInt precheck, narrowing only afterward. The [allocation-only native replay](https://github.com/resoltico/pdfconcat/actions/runs/38651034553) passed: 88 declared publish tests and 151 named passes in the healthy baseline; the actual subtraction mutant caused a baseline-known named failure rather than OOM, with settled processes, restored source and empty scratch.
- macOS partition 10 failed its unmutated baseline during a 37.3-second all-package event gap. The cause remains unknown. A [corrected diagnostic](https://github.com/resoltico/pdfconcat/actions/runs/38641300387) passed two focused and two full-module baselines without changing time limits. This does not erase the original failure.
- macOS partition 1 resolved every mutation without execution errors, but rejected `mutation-generated-pdf-reference-flush-equality` as unused in that report. The reviewed exception was not deleted solely to make that observation pass; its remaining platform consistency is unresolved.
- macOS partition 8 passed its mutation gate, then artifact upload failed with `ENOTFOUND`. This is not a passing hosted job or a complete accepted artifact.

The closing changes have focused native evidence, not a fresh full 36-partition mutation result. Earlier diagnostic failures and incomplete outcomes remain preserved. Deliberate coverage-failure controls are expected to appear red and are separate from ordinary CI failures.

## Disk sizing

The default two-worker mutation watch threshold is **14.125 GiB**: two 4 GiB post-command cache targets, 2 GiB per worker for source/build/scratch headroom, and one shared 2.125 GiB capped raw-output allowance. Four requested mutation workers use a **26.125 GiB** watch threshold. The four-worker fuzz watch is **5 GiB + 4 × 100 MiB**, approximately **5.39 GiB**. Native free-space checks reject below 1 GiB; that space is not reserved.

Storage aggregation over 23 verified diagnostic partitions observed at most **9,919,935,849 logical bytes (9.24 GiB)** and 75,819 entries, with 39 real cache trims finishing at or below 4 GiB. This is a sampled subset, not a proven peak or sufficient runner-size requirement. The failed 4 GiB virtual-memory request is not a measured 4 GiB disk write.

These are hard raw-output limits and sampled operating policies, **not a total filesystem quota**. Bootstrap/shared caches, retained histories, filesystem overhead, external writes, pagefile/swap backing and sampling/cancellation gaps remain outside a strict aggregate bound. No finite whole-workload maximum or guaranteed runner size has been established. See [contributor storage controls](../CONTRIBUTING.md).

## Branch disposition

Temporary diagnostic workflows are excluded from `main`; merging them would replace the real campaign workflow. Their commits and the two Dependabot proposals are preserved in a verified local Git bundle before branch deletion. Dependency proposals [#2](https://github.com/resoltico/pdfconcat/pull/2) and [#3](https://github.com/resoltico/pdfconcat/pull/3) are closed without applying new upgrades during closeout; the tested pins remain in place. No release or tag publication is part of this closeout.
