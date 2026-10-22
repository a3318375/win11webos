import { useEffect, useRef, useState } from 'react';
import { ArrowLeft, ArrowRight, ChevronLeft, ChevronRight, FolderOpen, Image as ImageIcon, Maximize, Monitor, RotateCw, ZoomIn, ZoomOut } from 'lucide-react';
import type { Entry } from './filesystem';
import './photo-viewer.css';

type Props = { entries:Entry[]; currentId:string; onSelect:(id:string)=>void; onShowFolder:(entry:Entry)=>void; onSetWallpaper:(entry:Entry)=>void; active:boolean };
const bounded = (value:number) => Math.max(.05,Math.min(5,value));
export default function PhotoViewer({entries,currentId,onSelect,onShowFolder,onSetWallpaper,active}:Props){
  const photo=entries.find(e=>e.id===currentId&&e.kind==='image'&&!e.deleted);
  const images=entries.filter(e=>e.kind==='image'&&!e.deleted&&e.parent===photo?.parent);
  const index=images.findIndex(e=>e.id===currentId);
  const [zoom,setZoom]=useState<number|null>(null);
  const [rotation,setRotation]=useState(0);
  const [offset,setOffset]=useState({x:0,y:0});
  const [size,setSize]=useState({width:0,height:0});
  const [area,setArea]=useState({width:0,height:0});
  const [loaded,setLoaded]=useState(false);
  const [failed,setFailed]=useState(false);
  const stageRef=useRef<HTMLDivElement>(null);
  const pan=useRef<{x:number;y:number;startX:number;startY:number}|null>(null);
  const rotated=rotation%180!==0;
  const fit=size.width&&area.width?Math.min(1,Math.max(.01,(area.width-40)/(rotated?size.height:size.width)),Math.max(.01,(area.height-40)/(rotated?size.width:size.height))):1;
  const scale=zoom??fit;
  const canPan=loaded&&((rotated?size.height:size.width)*scale>area.width||(rotated?size.width:size.height)*scale>area.height);
  const scaleRef=useRef(scale);scaleRef.current=scale;
  useEffect(()=>{const stage=stageRef.current;if(!stage)return;const observer=new ResizeObserver(([entry])=>setArea({width:entry.contentRect.width,height:entry.contentRect.height}));observer.observe(stage);return()=>observer.disconnect()},[!!photo]);
  useEffect(()=>{const stage=stageRef.current;if(!stage)return;const wheel=(e:WheelEvent)=>{e.preventDefault();setZoom(bounded(scaleRef.current*(e.deltaY<0?1.15:1/1.15)));setOffset({x:0,y:0})};stage.addEventListener('wheel',wheel,{passive:false});return()=>stage.removeEventListener('wheel',wheel)},[!!photo]);
  function change(direction:number){const next=images[index+direction];if(next)onSelect(next.id)}
  function zoomBy(factor:number){setZoom(bounded(scale*factor));setOffset({x:0,y:0})}
  function fitImage(){setZoom(null);setOffset({x:0,y:0})}
  useEffect(()=>{if(!active)return;const key=(e:KeyboardEvent)=>{if((e.target as HTMLElement).matches('input,textarea,select'))return;if(e.key==='ArrowLeft'){e.preventDefault();change(-1)}if(e.key==='ArrowRight'){e.preventDefault();change(1)}if(e.key==='+'||e.key==='='){e.preventDefault();zoomBy(1.25)}if(e.key==='-'){e.preventDefault();zoomBy(.8)}if(e.key==='0'){e.preventDefault();fitImage()}if(e.key==='1'){e.preventDefault();setZoom(1);setOffset({x:0,y:0})}};window.addEventListener('keydown',key);return()=>window.removeEventListener('keydown',key)});
  if(!photo)return <div className="photo-empty"><ImageIcon size={48}/><h2>图片已被移走或删除</h2><p>从文件资源管理器中选择另一张图片打开。</p></div>;
  return <div className="photo-viewer">
    <div className="photo-toolbar" role="toolbar" aria-label="图片工具栏"><div className="photo-toolbar-group"><button aria-label="上一张图片" title="上一张（←）" disabled={index<=0} onClick={()=>change(-1)}><ArrowLeft size={17}/></button><button aria-label="下一张图片" title="下一张（→）" disabled={index>=images.length-1} onClick={()=>change(1)}><ArrowRight size={17}/></button><span className="photo-counter">{index+1} / {images.length}</span></div><div className="photo-toolbar-group photo-zoom-tools"><button aria-label="缩小图片" title="缩小（−）" disabled={!loaded||scale<=.05} onClick={()=>zoomBy(.8)}><ZoomOut size={18}/></button><span className="photo-zoom">{Math.round(scale*100)}%</span><button aria-label="放大图片" title="放大（+）" disabled={!loaded||scale>=5} onClick={()=>zoomBy(1.25)}><ZoomIn size={18}/></button><span className="photo-tool-divider"/><button aria-label="适应窗口" title="适应窗口（0）" disabled={!loaded} className={zoom===null?'photo-tool-active':''} onClick={fitImage}><Maximize size={17}/></button><button aria-label="实际大小" title="实际大小（1）" disabled={!loaded} onClick={()=>{setZoom(1);setOffset({x:0,y:0})}}>1:1</button><button aria-label="旋转图片" title="顺时针旋转 90°" disabled={!loaded} onClick={()=>{setRotation(r=>(r+90)%360);setOffset({x:0,y:0})}}><RotateCw size={18}/></button></div><div className="photo-toolbar-group photo-file-tools"><button aria-label="在文件夹中显示图片" title="在文件夹中显示" onClick={()=>onShowFolder(photo)}><FolderOpen size={18}/></button><button aria-label="设为桌面背景" title="设为桌面背景" disabled={!loaded} onClick={()=>onSetWallpaper(photo)}><Monitor size={18}/></button></div></div>
    <div ref={stageRef} className={'photo-stage '+(canPan?'can-pan':'')} onContextMenu={e=>{e.preventDefault();e.stopPropagation()}} onDoubleClick={()=>zoom===1?fitImage():(setZoom(1),setOffset({x:0,y:0}))} onPointerDown={e=>{if(e.button!==0||!canPan)return;e.currentTarget.setPointerCapture(e.pointerId);pan.current={x:offset.x,y:offset.y,startX:e.clientX,startY:e.clientY}}} onPointerMove={e=>{if(pan.current)setOffset({x:pan.current.x+e.clientX-pan.current.startX,y:pan.current.y+e.clientY-pan.current.startY})}} onPointerUp={()=>pan.current=null} onPointerCancel={()=>pan.current=null} onLostPointerCapture={()=>pan.current=null}>
      {!failed&&<img key={photo.id+photo.src} src={photo.src||'/wallpaper.jpg'} alt={photo.name} draggable={false} onLoad={e=>{setSize({width:e.currentTarget.naturalWidth,height:e.currentTarget.naturalHeight});setLoaded(true);setFailed(false)}} onError={()=>{setFailed(true);setLoaded(false)}} style={{width:size.width||'auto',height:size.height||'auto',opacity:loaded?1:0,transform:`translate(-50%,-50%) translate(${offset.x}px,${offset.y}px) rotate(${rotation}deg) scale(${scale})`}}/>}
      {!loaded&&!failed&&<span className="photo-loading" role="status">正在打开图片…</span>}{failed&&<div className="photo-empty"><ImageIcon size={40}/><h2>无法打开这张图片</h2><p>文件可能已损坏，或图片资源不可用。</p></div>}
      {index>0&&<button className="photo-stage-prev" aria-label="查看上一张" onPointerDown={e=>e.stopPropagation()} onClick={()=>change(-1)}><ChevronLeft size={24}/></button>}{index<images.length-1&&<button className="photo-stage-next" aria-label="查看下一张" onPointerDown={e=>e.stopPropagation()} onClick={()=>change(1)}><ChevronRight size={24}/></button>}
    </div>
    {images.length>1&&<div className="photo-filmstrip" aria-label="当前文件夹的图片">{images.map(image=><button key={image.id} className={image.id===currentId?'current':''} aria-label={'查看 '+image.name} aria-pressed={image.id===currentId} title={image.name} onClick={()=>onSelect(image.id)}><img src={image.src||'/wallpaper.jpg'} alt="" draggable={false}/></button>)}</div>}
    <footer className="photo-status"><span title={photo.name}>{photo.name}</span><span>{loaded?`${size.width} × ${size.height} 像素`:'—'}<i/>旋转 {rotation}°</span><small>滚轮缩放 · 放大后拖动查看</small></footer>
  </div>;
}
