# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Ervins Strauhmanis
"""Original fictional artwork and image-only PDF; no camera/private metadata."""
import argparse
from pathlib import Path
from PIL import Image,ImageDraw,ImageFont
from reportlab.pdfgen import canvas
from reportlab.lib.utils import ImageReader
from reportlab import rl_config
p=argparse.ArgumentParser();p.add_argument('--output-dir',type=Path,required=True);a=p.parse_args();a.output_dir.mkdir(parents=True,exist_ok=True)
corpus=Path(__file__).resolve().parents[1];font=ImageFont.truetype(str(corpus.parents[2]/'typeset/fontdata/NotoSans-Regular.ttf'),24)
rl_config.useA85=0
images=[]
for name,mode,size,title in [('invoice-rgb','RGB',(600,800),'SYNTHETIC INVOICE'),('receipt-gray','L',(800,600),'FICTIONAL RECEIPT')]:
 image=Image.new(mode,size,'white');d=ImageDraw.Draw(image);w,h=size
 d.rectangle((0,0,79,79),fill=(220,30,30) if mode=='RGB' else 20)
 d.rectangle((w-80,h-80,w-1,h-1),fill=(20,140,70) if mode=='RGB' else 90)
 d.text((90,35),title,font=font,fill='black');d.text((40,120),'PUBLIC CORPUS - NO CUSTOMER DATA',font=font,fill='black')
 for n in range(8):d.text((40,180+n*38),f'Sample {n+1:02d}   units {n+2}   EUR {(n+2)*3}.00',font=font,fill='black')
 d.rectangle((40,h-85,w-100,h-60),fill=(35,70,110) if mode=='RGB' else 140)
 image.save(a.output_dir/(name+'.png'))
 if mode=='RGB':image.save(a.output_dir/(name+'.jpg'),quality=92,subsampling=0);images.append((a.output_dir/(name+'.jpg'),(600,800)))
 else:images.append((a.output_dir/(name+'.png'),(800,600)))
c=canvas.Canvas(str(a.output_dir/'scan-invoice.pdf'),pagesize=(600,800),pageCompression=1,invariant=1);c.setTitle('Synthetic invoice and receipt');c.setAuthor('PDFConcat fixture authors');c.setSubject('Original fictional raster artwork; MPL-2.0');c.setCreator('PDFConcat corpus sources / Pillow and ReportLab')
for path,size in images:c.setPageSize(size);c.drawImage(ImageReader(str(path)),0,0,width=size[0],height=size[1]);c.showPage()
c.save()
