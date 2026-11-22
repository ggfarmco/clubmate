import re,sys
from datetime import datetime
rows=[]
for line in open(sys.argv[1]):
    m=re.match(r'\S+ (\d\d:\d\d:\d\d\.\d+).*\[com\.clubmate\.companion\.perf:(frame|step)\] (.*)',line)
    if not m: continue
    rows.append((datetime.strptime(m.group(1)[:12],'%H:%M:%S.%f').timestamp(),m.group(2),m.group(3).strip()))
moves={}
for i,(t,k,msg) in enumerate(rows):
    if k!='step' or ' begin ' not in msg: continue
    kind=msg.split()[1]
    end=next((tt for tt,kk,mm in rows[i+1:] if kk=='step' and mm.startswith("step "+kind+" end")),None)
    if end is None: continue
    hitches=[(int(mm.split()[1]),int(mm.split()[4]),(tt-t)/max(end-t,0.001)) for tt,kk,mm in rows if kk=='frame' and mm.startswith('hitch') and t<=tt<=end+0.6]
    moves.setdefault(kind,[]).append((end-t,len(hitches),sum(d for _,d,_ in hitches),max([g for g,_,_ in hitches],default=0),sum(d for _,d,r in hitches if r<0.5),sum(d for _,d,r in hitches if r>=0.5)))
print('| move | n | s per move | hitches/move | dropped frames/move | worst frame ms | dropped 1st half | dropped 2nd half |')
print('|---|---|---|---|---|---|---|---|')
for kind,ms in moves.items():
    n=len(ms)
    print(f'| {kind} | {n} | {sum(m[0] for m in ms)/n:.2f} | {sum(m[1] for m in ms)/n:.1f} | {sum(m[2] for m in ms)/n:.1f} | {max(m[3] for m in ms)} | {sum(m[4] for m in ms)/n:.1f} | {sum(m[5] for m in ms)/n:.1f} |')
    print('|  per run | | | '+' '.join(str(m[1]) for m in ms)+' | '+' '.join(str(m[2]) for m in ms)+' | '+' '.join(str(m[3]) for m in ms)+' |')
