import {useEffect,useRef,useState,type PointerEvent,type KeyboardEvent,type MouseEvent} from 'react';
import {cloudApi,type MountFile} from './cloud-api';

type Clipboard={account:string;parent:string;items:MountFile[];cut:boolean};
export default function useCloudSelection(account:string,parent:string,files:MountFile[],writable:boolean,refresh:()=>void,notify:(text:string)=>void,open:(item:MountFile)=>void,download:(item:MountFile)=>void,transfer?: (items:MountFile[])=>void){
 const [ids,setIDs]=useState<Set<string>>(new Set()),[clipboard,setClipboard]=useState<Clipboard|null>(null),[busy,setBusy]=useState(false),[menu,setMenu]=useState<{x:number;y:number;file:boolean}|null>(null),[dialog,setDialog]=useState<'rename'|'mkdir'|'trash'|null>(null),[name,setName]=useState(''),[error,setError]=useState(''),[box,setBox]=useState<{left:number;top:number;width:number;height:number}|null>(null);
 const container=useRef<HTMLDivElement>(null),anchor=useRef(''),suppress=useRef(false),drag=useRef<{x:number;y:number;startX:number;startY:number;clientX:number;clientY:number;base:Set<string>;active:boolean;pointer:number}|null>(null);
 const selected=files.filter(f=>ids.has(f.id));
 useEffect(()=>{setIDs(new Set());setMenu(null);setDialog(null);anchor.current='';container.current?.focus({preventScroll:true})},[account,parent]);
 useEffect(()=>{if(!dialog)container.current?.focus({preventScroll:true})},[dialog]);
 const visibleIDs=files.map(f=>f.id).join('\0');
 useEffect(()=>{const allowed=new Set(files.map(f=>f.id));setIDs(current=>{if([...current].every(id=>allowed.has(id)))return current;return new Set([...current].filter(id=>allowed.has(id)))})},[visibleIDs]);
 useEffect(()=>{const close=()=>setMenu(null);window.addEventListener('blur',close);return()=>window.removeEventListener('blur',close)},[]);
 function select(e:MouseEvent,item:MountFile){
  if(suppress.current){suppress.current=false;return}
  if(e.shiftKey&&anchor.current){const a=files.findIndex(f=>f.id===anchor.current),b=files.indexOf(item);const range=files.slice(Math.max(0,Math.min(a,b)),Math.max(a,b)+1).map(f=>f.id);setIDs(new Set(e.ctrlKey?[...ids,...range]:range))}
  else if(e.ctrlKey||e.metaKey){setIDs(current=>{const next=new Set(current);next.has(item.id)?next.delete(item.id):next.add(item.id);return next});anchor.current=item.id}
  else {setIDs(new Set([item.id]));anchor.current=item.id}
 }
 function copy(cut=false){if(!selected.length||busy)return;if(cut&&!writable){notify('此网盘暂未接入剪切');return}setClipboard({account,parent,items:selected,cut});setMenu(null);container.current?.focus({preventScroll:true});notify(`已${cut?'剪切':'复制'} ${selected.length} 项，请进入目标文件夹粘贴`)}
 async function execute(action:string,items:MountFile[]=selected){
  if(!writable||busy)return;
  if(action==='paste'&&(!clipboard||clipboard.account!==account)){notify('暂不支持跨网盘粘贴，请在同一存储内操作');return}
  const input=action==='paste'?{action:clipboard!.cut?'move':'copy',parent,sourceParent:clipboard!.parent,ids:clipboard!.items.map(f=>f.id)}:{action,parent,sourceParent:parent,ids:items.map(f=>f.id),name};
  setBusy(true);setError('');setMenu(null);
  try{const result=await cloudApi.action(account,input);if(action==='paste'&&clipboard?.cut)setClipboard(null);setDialog(null);setIDs(new Set());notify(`操作完成 · ${result.completed} 项${action==='trash'?'已移入回收站':''}`);refresh()}
  catch(e){const text=(e as Error).message;setError(text);notify(text);refresh()}
  finally{setBusy(false)}
 }
 function showDialog(kind:'rename'|'mkdir'|'trash'){if(!writable||busy)return;if(kind==='rename'&&selected.length!==1)return;if(kind==='trash'&&!selected.length)return;setName(kind==='rename'?selected[0].name:'新建文件夹');setError('');setDialog(kind);setMenu(null)}
 function key(e:KeyboardEvent){
  if((e.target as HTMLElement).closest('input,textarea,select,[contenteditable="true"]'))return;
  const mod=e.ctrlKey||e.metaKey;
  if(e.key==='Escape'){setMenu(null);if(!busy)setDialog(null);setIDs(new Set());setClipboard(c=>c?.cut?null:c);return}
  if(dialog||busy)return;
  if(mod&&['a','c','x','v'].includes(e.key.toLowerCase())){e.preventDefault();switch(e.key.toLowerCase()){case'a':setIDs(new Set(files.map(f=>f.id)));break;case'c':copy();break;case'x':copy(true);break;case'v':void execute('paste');break}return}
  if(e.key==='F2'){e.preventDefault();showDialog('rename')}
  if(e.key==='Delete'){e.preventDefault();showDialog('trash')}
  if(mod&&e.shiftKey&&e.key.toLowerCase()==='n'){e.preventDefault();showDialog('mkdir')}
 }
 function context(e:MouseEvent,item?:MountFile){e.preventDefault();e.stopPropagation();if(busy||dialog)return;if(item&&!ids.has(item.id)){setIDs(new Set([item.id]));anchor.current=item.id}else if(!item)setIDs(new Set());const bounds=e.currentTarget.closest('.cloud-drive')!.getBoundingClientRect();setMenu({x:Math.max(8,Math.min(e.clientX-bounds.left,bounds.width-220)),y:Math.max(8,Math.min(e.clientY-bounds.top,bounds.height-405)),file:!!item})}
 function pointerDown(e:PointerEvent){
  if(e.button!==0||busy||dialog||(e.target as HTMLElement).closest('.cd-item,button,a,input,select,.cd-table-head'))return;
  const el=container.current!;el.focus({preventScroll:true});const r=el.getBoundingClientRect();setMenu(null);drag.current={x:e.clientX-r.left+el.scrollLeft,y:e.clientY-r.top+el.scrollTop,startX:e.clientX,startY:e.clientY,clientX:e.clientX,clientY:e.clientY,base:e.ctrlKey||e.metaKey?new Set(ids):new Set(),active:false,pointer:e.pointerId};e.stopPropagation();
 }
 function updateBox(){
  const d=drag.current,el=container.current;if(!d||!el)return;
  if(!d.active&&Math.hypot(d.clientX-d.startX,d.clientY-d.startY)<4)return;d.active=true;
  const r=el.getBoundingClientRect();const x=Math.max(0,Math.min(r.width,d.clientX-r.left))+el.scrollLeft,y=Math.max(0,Math.min(r.height,d.clientY-r.top))+el.scrollTop;
  const rect={left:Math.min(d.x,x),top:Math.min(d.y,y),width:Math.abs(x-d.x),height:Math.abs(y-d.y)};setBox(rect);
  const next=new Set(d.base);el.querySelectorAll<HTMLElement>('[data-file-id]').forEach(row=>{const b=row.getBoundingClientRect(),left=b.left-r.left+el.scrollLeft,top=b.top-r.top+el.scrollTop;if(left<rect.left+rect.width&&left+b.width>rect.left&&top<rect.top+rect.height&&top+b.height>rect.top)next.add(row.dataset.fileId!)});setIDs(next);
 }
 useEffect(()=>{
  let frame=0;const move=(e:globalThis.PointerEvent)=>{if(!drag.current)return;drag.current.clientX=e.clientX;drag.current.clientY=e.clientY;updateBox();if(drag.current.active){e.preventDefault();if(!frame)frame=requestAnimationFrame(tick)}};
  const tick=()=>{frame=0;const d=drag.current,el=container.current;if(d?.active&&el){const r=el.getBoundingClientRect();const delta=d.clientY>r.bottom-28?12:d.clientY<r.top+28?-12:0;if(delta){el.scrollTop+=delta;updateBox()}frame=requestAnimationFrame(tick)}};
  const finish=()=>{if(!drag.current)return;suppress.current=drag.current.active;if(!drag.current.active&&!drag.current.base.size)setIDs(new Set());drag.current=null;setBox(null);setTimeout(()=>{suppress.current=false},0)};
  window.addEventListener('pointermove',move,{passive:false});window.addEventListener('pointerup',finish);window.addEventListener('pointercancel',finish);return()=>{cancelAnimationFrame(frame);window.removeEventListener('pointermove',move);window.removeEventListener('pointerup',finish);window.removeEventListener('pointercancel',finish)};
 },[]);
 const pasteEnabled=!!clipboard&&clipboard.account===account&&writable&&!busy;
 const overlays=<>
 {menu&&<><div className="cd-menu-dismiss" onPointerDown={e=>{e.stopPropagation();setMenu(null)}} onContextMenu={e=>{e.preventDefault();setMenu(null)}}/><div className="cd-context" role="menu" style={{left:menu.x,top:menu.y}} onPointerDown={e=>e.stopPropagation()}>
 {menu.file&&<><button role="menuitem" disabled={selected.length!==1} onClick={()=>{setMenu(null);if(selected[0])open(selected[0])}}>打开<span>Enter</span></button><button role="menuitem" disabled={selected.some(f=>f.directory)} onClick={()=>{setMenu(null);selected.forEach(download)}}>下载到浏览器</button><button role="menuitem" disabled={!transfer} onClick={()=>{setMenu(null);transfer?.(selected)}}>下载到本地…</button><hr/><button role="menuitem" onClick={()=>copy()}>复制<span>Ctrl+C</span></button><button role="menuitem" disabled={!writable} onClick={()=>copy(true)}>剪切<span>Ctrl+X</span></button></>}
 <button role="menuitem" disabled={!pasteEnabled} onClick={()=>void execute('paste')}>粘贴{clipboard&&<span>{clipboard.items.length} 项 · Ctrl+V</span>}</button>
 <button role="menuitem" disabled={!writable} onClick={()=>showDialog('mkdir')}>新建文件夹<span>Ctrl+Shift+N</span></button>
 {menu.file&&<><hr/><button role="menuitem" disabled={!writable||selected.length!==1} onClick={()=>showDialog('rename')}>重命名<span>F2</span></button><button role="menuitem" className="danger" disabled={!writable} onClick={()=>showDialog('trash')}>移入回收站<span>Delete</span></button></>}
 <hr/><button role="menuitem" onClick={()=>{setMenu(null);refresh()}}>刷新</button></div></>}
 {dialog&&<div className="cd-overlay"><form className="cd-dialog cd-action-dialog" role="dialog" aria-modal="true" aria-label={dialog==='rename'?'重命名':dialog==='mkdir'?'新建文件夹':'移入回收站'} onSubmit={e=>{e.preventDefault();void execute(dialog)}}><h2>{dialog==='rename'?'重命名':dialog==='mkdir'?'新建文件夹':`将 ${selected.length} 项移入回收站？`}</h2>{dialog==='trash'?<p>夸克文件可在网盘回收站恢复；本地文件保留在挂载目录的 .cloud-trash 文件夹中。</p>:<label>名称<input autoFocus value={name} disabled={busy} onFocus={e=>{const end=dialog==='rename'&&!selected[0]?.directory?name.lastIndexOf('.'):name.length;e.target.setSelectionRange(0,end>0?end:name.length)}} onChange={e=>setName(e.target.value)} required maxLength={255}/></label>}{error&&<p role="alert" className="ld-error">{error}</p>}<div><button type="button" disabled={busy} onClick={()=>setDialog(null)}>取消</button><button className="cd-primary" disabled={busy} type="submit">{busy?'处理中…':dialog==='trash'?'移入回收站':'确定'}</button></div></form></div>}
 </>;
 return {ids,selected,clipboard,busy,menu,container,box,select,key,context,pointerDown,copy,execute,showDialog,overlays,pasteEnabled,clear:()=>{if(!suppress.current)setIDs(new Set())},closeMenu:()=>setMenu(null)};
}
