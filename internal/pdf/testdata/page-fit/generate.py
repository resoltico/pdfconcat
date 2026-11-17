#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 Ervins Strauhmanis
"""Synthetic page/destination fixtures independent of the production PDF writer."""
from pathlib import Path
import sys

def pdf(sizes,target_index=0,origin_index=0,rotate=0,unit=1,crop=None):
 objects={1:b'<< /Type /Catalog /Pages 2 0 R >>',3:b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'}
 pages=[4+2*i for i in range(len(sizes))];link=4+2*len(sizes)
 objects[2]=f'<< /Type /Pages /Count {len(pages)} /Kids ['.encode()+b' '.join(f'{p} 0 R'.encode() for p in pages)+b'] >>'
 for i,((w,h),p) in enumerate(zip(sizes,pages)):
  extras=f' /Rotate {rotate} /UserUnit {unit}'
  if crop is not None:extras+=' /CropBox ['+' '.join(str(x) for x in crop)+']'
  if i==origin_index:extras+=f' /Annots [{link} 0 R]'
  objects[p]=f'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 {w} {h}] /Resources << /Font << /F1 3 0 R >> >> /Contents {p+1} 0 R{extras} >>'.encode()
  content=f'BT /F1 10 Tf 20 40 Td (Synthetic page {i+1}) Tj ET\n'.encode();objects[p+1]=f'<< /Length {len(content)} >>\nstream\n'.encode()+content+b'endstream'
 objects[link]=f'<< /Type /Annot /Subtype /Link /Rect [10 10 30 30] /P {pages[origin_index]} 0 R /Dest [{pages[target_index]} 0 R /XYZ 20 30 null] >>'.encode()
 data=bytearray(b'%PDF-1.7\n');offsets={}
 for n,body in sorted(objects.items()):offsets[n]=len(data);data+=f'{n} 0 obj\n'.encode()+body+b'\nendobj\n'
 start=len(data);size=max(objects)+1;data+=f'xref\n0 {size}\n0000000000 65535 f \n'.encode()
 for n in range(1,size):data+=(f'{offsets[n]:010d} 00000 n \n' if n in offsets else '0000000000 00000 f \n').encode()
 data+=f'trailer\n<< /Size {size} /Root 1 0 R >>\nstartxref\n{start}\n%%EOF\n'.encode();return bytes(data)
root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=True)
cases=[('exact-letter',[(612,792)],0,0,0,1,None),('rounded-a4',[(595.28,841.89)],0,0,0,1,None),('integer-a4',[(595,842)],0,0,0,1,None),('mixed-to-kept',[(100,100),(200,100)],0,1,0,1,None),('mixed-to-transform',[(100,100),(200,100)],1,0,0,1,None),('rotated-letter',[(792,612)],0,0,90,1,None),('unit-letter',[(306,396)],0,0,0,2,None),('crop-letter',[(700,900)],0,0,0,1,[10,20,622,812])]
for name,sizes,target,origin,rotate,unit,crop in cases:(root/(name+'.pdf')).write_bytes(pdf(sizes,target,origin,rotate,unit,crop))


def blank_native(media,unit):
    bodies=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',f'<< /Type /Page /Parent 2 0 R /MediaBox [{media}] /UserUnit {unit} /Resources << >> >>'.encode()]
    data=bytearray(b'%PDF-1.7\n');offsets=[]
    for i,body in enumerate(bodies,1):
        offsets.append(len(data));data+=f'{i} 0 obj\n'.encode()+body+b'\nendobj\n'
    start=len(data);data+=b'xref\n0 4\n0000000000 65535 f \n'
    for n in offsets:data+=f'{n:010d} 00000 n \n'.encode()
    data+=f'trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n{start}\n%%EOF\n'.encode()
    return bytes(data)

(root/'tiny-raw-large-unit.pdf').write_bytes(blank_native('0 0 0.0000000000001 0.0000000000001','1000000000000000'))
(root/'large-origin.pdf').write_bytes(blank_native('10000000000000000 10000000000000000 10000000000000600 10000000000000800','1'))
