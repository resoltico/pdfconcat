# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Ervins Strauhmanis
"""Independent-producer, realistic vector/table/form sources for field tests."""
from pathlib import Path
from reportlab.pdfgen import canvas
from reportlab.lib.pagesizes import A4,landscape
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
import argparse
p=argparse.ArgumentParser();p.add_argument("--output-dir",type=Path,required=True);a=p.parse_args()
root=a.output_dir;root.mkdir(parents=True,exist_ok=True)
corpus=Path(__file__).resolve().parents[1]
pdfmetrics.registerFont(TTFont('FieldNoto',str(corpus.parents[2]/'typeset/fontdata/NotoSans-Regular.ttf')))
c=canvas.Canvas(str(root/'business-report.pdf'),pagesize=A4,pageCompression=1,invariant=1)
c.setAuthor('PDFConcat fixture authors');c.setSubject('Fictional corpus material; original content MPL-2.0');c.setCreator('PDFConcat corpus sources / ReportLab 4.4.9')
c.setTitle('Field packet: project evidence report')
def heading(title,sub,width,height):
 c.setFillColorRGB(.10,.18,.28);c.rect(0,height-96,width,96,fill=1,stroke=0)
 c.setFillColorRGB(1,1,1);c.setFont('FieldNoto',20);c.drawString(36,height-45,title)
 c.setFont('FieldNoto',10);c.drawString(36,height-70,sub)
 c.setFillColorRGB(0,0,0)
def footer(n):
 c.setFont('FieldNoto',8);c.drawString(36,12,'Independent source page '+str(n))
heading('Project evidence report','Pielikums Ā ļ ķ — Ελληνικά Кириллица',*A4)
c.bookmarkPage('summary');c.addOutlineEntry('Report summary','summary',level=0)
c.setFont('FieldNoto',12)
for y,line in [(710,'An ordinary source with an embedded Unicode font, vector table,'),(690,'native outline entries and existing footer content.'),(660,'Item                           Quantity        Amount EUR')]:c.drawString(36,y,line)
for i in range(12):
 y=630-i*28
 c.setFillColorRGB(.94,.96,.98) if i%2==0 else c.setFillColorRGB(1,1,1)
 c.rect(36,y-8,A4[0]-72,25,fill=1,stroke=0);c.setFillColorRGB(0,0,0)
 c.drawString(44,y,'Evidence item %02d'%(i+1));c.drawRightString(390,y,str(i+2));c.drawRightString(A4[0]-45,y,'%.2f'%((i+2)*17.35))
c.linkURL('https://example.invalid/evidence',(36,190,280,210),relative=0)
c.setFont('FieldNoto',10);c.drawString(36,196,'Example evidence link (not contacted during testing)')
footer(1);c.showPage()
w,h=landscape(A4);c.setPageSize((w,h));heading('Measured comparison','Mixed page size / landscape source',w,h)
c.bookmarkPage('chart');c.addOutlineEntry('Comparison chart','chart',level=0)
for i,value in enumerate([120,280,190,350,220]):
 x=65+i*145;c.setFillColorRGB(.15,.45,.62);c.rect(x,100,85,value,fill=1,stroke=0)
 c.setFillColorRGB(0,0,0);c.setFont('FieldNoto',10);c.drawString(x,80,'Group '+str(i+1));c.drawString(x,value+112,str(value))
footer(2);c.showPage()
c.setPageSize(A4);heading('Supporting notes','Text and vector source, no scanned-page dependence',*A4)
c.bookmarkPage('notes');c.addOutlineEntry('Supporting notes','notes',level=0)
c.setFont('FieldNoto',12)
for i in range(18):c.drawString(36,710-i*28,'Note %02d: verify source text, bookmarks and physical footer placement.'%(i+1))
footer(3);c.save()
c=canvas.Canvas(str(root/'approval-form-base.pdf'),pagesize=A4,invariant=1)
c.setAuthor('PDFConcat fixture authors');c.setSubject('Fictional corpus material; original content MPL-2.0');c.setCreator('PDFConcat corpus sources / ReportLab 4.4.9')
c.setTitle('Field packet: review form');c.setFont('Helvetica',18);c.drawString(50,770,'Review approval')
c.setFont('Helvetica',11);c.drawString(50,720,'Reviewer:')
c.acroForm.textfield(name='reviewer',value='Field tester',x=140,y=705,width=260,height=28,borderWidth=1)
c.drawString(50,650,'Approved:');c.acroForm.checkbox(name='approved',checked=True,x=140,y=642,size=18)
c.showPage();c.setFont('Helvetica',14);c.drawString(50,740,'Supporting page without widgets');c.save()
print('Created independent report and interactive form sources.')
