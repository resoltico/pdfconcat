#!/usr/bin/env python3
"""Generate synthetic classic-xref fixtures; no application or PDF library needed."""
from pathlib import Path
import sys

def document(zero=False, reference=False, dormant=False, older=False):
    content=b"BT /F1 12 Tf 50 700 Td (Synthetic xref evidence) Tj ET\n"
    objects={1:b"<< /Type /Catalog /Pages 2 0 R"+(b" /Probe 6 0 R" if reference else b"")+b" >>",2:b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",3:b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",4:b"<< /Length "+str(len(content)).encode()+b" >>\nstream\n"+content+b"endstream",5:b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
    if older: objects[6]=b"(OLD-DEFINITION-MUST-NOT-RETURN)"
    if dormant: objects[7]=b"<< /Probe 6 0 R >>"
    data=bytearray(b"%PDF-1.7\n%\xe2\xe3\xcf\xd3\n");offsets={}
    for n,body in sorted(objects.items()):
        offsets[n]=len(data);data+=f"{n} 0 obj\n".encode()+body+b"\nendobj\n"
    xref=len(data);size=max(max(objects)+1,7 if zero else 6)
    data+=f"xref\n0 {size}\n0000000000 65535 f \n".encode()
    for n in range(1,size):
        data+=(f"{offsets[n]:010d} 00000 n \n".encode() if n in offsets else b"0000000000 00000 n \n")
    data+=f"trailer\n<< /Size {size} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n".encode()
    if older:
        newxref=len(data)
        data+=f"xref\n6 1\n0000000000 00000 n \ntrailer\n<< /Size 7 /Root 1 0 R /Prev {xref} >>\nstartxref\n{newxref}\n%%EOF\n".encode()
    return bytes(data)

root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=True)
for name,kwargs in [('clean.pdf',{}),('unreferenced-zero.pdf',{'zero':True}),('referenced-zero.pdf',{'zero':True,'reference':True}),('unreachable-ref-zero.pdf',{'zero':True,'dormant':True}),('incremental-shadow-zero.pdf',{'older':True})]:
    (root/name).write_bytes(document(**kwargs))
