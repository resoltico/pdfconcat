#!/usr/bin/env python3
"""One precise valid incremental revision of the owned ReportLab form fixture.

Not a general PDF editor. The source must be the unmodified two-page fixture.
"""
import argparse,json,pathlib,re,subprocess
p=argparse.ArgumentParser();p.add_argument('source');p.add_argument('output');a=p.parse_args()
source=pathlib.Path(a.source);output=pathlib.Path(a.output);original=source.read_bytes()
assert not output.exists()
doc=json.loads(subprocess.check_output(['qpdf','--json',str(source)]));table=doc['qpdf'][1]
page=table['obj:'+doc['pages'][1]['object']]['value'];ref=page['/Contents'];number,generation,_=ref.split();assert generation=='0'
resources=page['/Resources']
while isinstance(resources,str):resources=table['obj:'+resources]['value']
fonts=resources['/Font']
while isinstance(fonts,str):fonts=table['obj:'+fonts]['value']
assert '/F1'in fonts
m=re.search(rb'trailer\s*(<<.*?>>)\s*startxref\s*(\d+)\s*%%EOF\s*$',original,re.S);assert m and b'/Prev'not in m[1]
payload=b'BT /F1 14 Tf 50 740 Td (Supporting page: FINAL REVISION) Tj ET\n'
object_offset=len(original)+1
entry=b'\n'+number.encode()+b' 0 obj\n<< /Length '+str(len(payload)).encode()+b' >>\nstream\n'+payload+b'endstream\nendobj\n'
xref_offset=len(original)+len(entry)
trailer=m[1][:-2]+b' /Prev '+m[2]+b' >>'
suffix=b'xref\n'+number.encode()+b' 1\n'+('%010d 00000 n \n'%object_offset).encode()+b'trailer\n'+trailer+b'\nstartxref\n'+str(xref_offset).encode()+b'\n%%EOF\n'
output.write_bytes(original+entry+suffix)
