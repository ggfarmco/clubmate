import re,sys
from datetime import datetime
rows=[]
for line in open(sys.argv[1]):
    m=re.match(r'\S+ (\d\d:\d\d:\d\d\.\d+).*\[com\.clubmate\.companion\.perf:(request|step)\] (.*)',line)
    if not m: continue
    t=datetime.strptime(m.group(1)[:12],'%H:%M:%S.%f').timestamp()
    rows.append((t,m.group(2),m.group(3).strip()))
open_={}
reqs=[]
for t,k,msg in rows:
    if k=='request':
        kind,name=msg.split(' ',1)
        if kind=='begin': open_.setdefault(name,[]).append(len(reqs)); reqs.append([t,None,name])
        elif open_.get(name): reqs[open_[name].pop(0)][1]=t
steps=[(t,msg) for t,k,msg in rows if k=='step']
limit=int(sys.argv[2]) if len(sys.argv)>2 else 3
seen={}
for i,(t,msg) in enumerate(steps):
    if ' begin ' not in msg: continue
    kind=msg.split()[1]
    seen[kind]=seen.get(kind,0)+1
    if seen[kind]>limit: continue
    end=next((tt for tt,mm in steps[i+1:] if ' end ' in mm),t)
    nxt=next((tt for tt,mm in steps[i+1:] if ' begin ' in mm),t+5)
    print(f'== {msg}  (wait {end-t:.2f}s)')
    for b,e,n in reqs:
        if t-0.05<=b<nxt:
            d=f'{(e-b)*1000:.0f}ms' if e else 'open'
            print(f'  +{(b-t)*1000:6.0f}ms {d:>7} {"<end" if b<end else "after"} {re.sub(r"[\\\\]134/","/",n)[:150]}')
