"""REAL experiment: transfer function counts -> px near the SCREEN EDGE and with
the pointer held at various x, plus the edge-clamp probe (the servo's #1 trap)."""
import subprocess, sys
import Xlib.display, Xlib.X as X
from Xlib.ext import xtest
d=Xlib.display.Display(sys.argv[1] if len(sys.argv)>1 else ':77')
root=d.screen().root
def pos(): p=root.query_pointer(); return p.root_x,p.root_y
def rel(dx,dy=0): xtest.fake_input(d,X.MotionNotify,detail=1,x=dx,y=dy); d.sync()
def warp(x,y): root.warp_pointer(x,y); d.sync()
sh=lambda c: subprocess.run(c,shell=True,capture_output=True,text=True).stdout.strip()
print("geometry:", sh("xdpyinfo -display %s | grep dimensions"%sys.argv[1]))
print("--- transfer vs pointer x-position (walk to x, then probe +/-100 from there) ---")
for x0 in (60,300,960,1500,1860,1919):
    warp(x0,540); a=pos(); rel(-100); b=pos(); rel(100); c=pos()
    print("  at x=%4d : -100 -> moved %5d (%.3f) ; +100 -> moved %5d (%.3f)"%(x0,b[0]-a[0],(b[0]-a[0])/-100.0,c[0]-b[0],(c[0]-b[0])/100.0))
print("--- edge clamp: how far right can a big command go, and what does the servo see? ---")
for start in (0,960,1800):
    warp(start,540); a=pos(); rel(127); rel(127); rel(127); rel(127); b=pos()
    print("  start x=%4d, sent +508 counts -> x=%4d (moved %4d, clamp loss %d px)"%(start,b[0],b[0]-a[0],508-(b[0]-a[0])))
print("--- does the CH9329's own report rate matter? estimate: 9600 8N1 = %d bytes/s -> %.1f frames/s of 10-byte rel frames"%(9600//10, (9600//10)/10.0))
