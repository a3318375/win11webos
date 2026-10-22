import { useEffect, useRef, useState } from 'react';
import { ArrowLeft, Check, ChevronRight, Cloud, Folder, HardDrive, Link2, ScanLine, WandSparkles, X } from 'lucide-react';
import './link-drive-dialog.css';

export const providers = [
 {id:'115',name:'115 网盘',letter:'115',color:'#ee9235',description:'使用访问令牌与刷新令牌'},
 {id:'baidu',name:'百度网盘',letter:'B',color:'#3984ef',description:'使用刷新令牌，可选访问令牌'},
 {id:'quark',name:'夸克网盘',letter:'Q',color:'#655ce8',description:'使用 Cookie 或模拟扫码授权'},
 {id:'nas',name:'本地 NAS',letter:'N',color:'#17958c',description:'选择服务器上的本地根目录'},
];
type Draft = {name:string;access:string;refresh:string;cookie:string;root:string;cache:string;download:string;deletion:string};
export type DriveConfig = {provider:string;root:string;cache:number;download?:string;deletion?:string};
export type NewDrive = {name:string;letter:string;color:string;config:DriveConfig};
function defaults(id:string):Draft {return {name:'',access:'',refresh:'',cookie:'',root:id==='nas'?'':id==='baidu'?'/':'0',cache:id==='nas'?'0':'30',download:id==='115'?'redirect':'proxy',deletion:'trash'}}
export default function LinkDriveDialog({existingNames,onClose,onAdd}:{existingNames:string[];onClose:()=>void;onAdd:(drive:NewDrive)=>string|void}){
 const [provider,setProvider]=useState('115'),[step,setStep]=useState(1),[drafts,setDrafts]=useState<Record<string,Draft>>({}),[errors,setErrors]=useState<Record<string,string>>({}),[auth,setAuth]=useState(false),[authorized,setAuthorized]=useState(false),[browsing,setBrowsing]=useState(false),[directory,setDirectory]=useState('/mnt/media'),[advanced,setAdvanced]=useState(false);
 const panel=useRef<HTMLFormElement>(null),heading=useRef<HTMLHeadingElement>(null);
 const current=providers.find(p=>p.id===provider)!;const draft=drafts[provider]||defaults(provider);const nas=provider==='nas';
 function update(key:keyof Draft,value:string){setDrafts(ds=>({...ds,[provider]:{...(ds[provider]||defaults(provider)),[key]:value}}));setErrors(es=>({...es,[key]:''}));setAuthorized(false)}
 useEffect(()=>{heading.current?.focus()},[step]);
 function changeProvider(id:string){setProvider(id);setErrors({});setAuth(false);setBrowsing(false);setAuthorized(false)}
 function submit(){
  const next:Record<string,string>={};
  if(!draft.name.trim())next.name='请输入账号名称';else if(existingNames.some(n=>n.toLowerCase()===draft.name.trim().toLowerCase()))next.name='此名称已存在，请换一个名称';
  if(provider==='115'&&!draft.access.trim())next.access='请填写访问令牌，或使用模拟获取';
  if((provider==='115'||provider==='baidu')&&!draft.refresh.trim())next.refresh='请填写刷新令牌，或使用模拟获取';
  if(provider==='quark'&&!draft.cookie.trim())next.cookie='请填写 Cookie，或体验模拟扫码';
  if(nas&&!draft.root.trim())next.root='请选择或填写本地根目录';
  else if((nas||provider==='baidu')&&!draft.root.startsWith('/'))next.root='根目录路径应以 / 开头';
  else if((provider==='115'||provider==='quark')&&!/^\d+$/.test(draft.root))next.root='根目录 ID 应为非负整数';
  if(!/^\d+$/.test(draft.cache)||Number(draft.cache)>10080)next.cache='请输入 0～10080 之间的整数';
  setErrors(next);
  if(Object.keys(next).length){if(next.root||next.cache)setAdvanced(true);requestAnimationFrame(()=>panel.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus());return}
  const result=onAdd({name:draft.name.trim(),letter:current.letter,color:current.color,config:{provider,root:draft.root,cache:Number(draft.cache),...(!nas?{download:draft.download,deletion:draft.deletion}:{})}});
  if(result)setErrors({form:result});
 }
 function simulateAuth(){setDrafts(ds=>({...ds,[provider]:{...(ds[provider]||defaults(provider)),...(provider==='quark'?{cookie:'demo_cookie=quark_preview_only'}:{access:`demo_${provider}_access_token`,refresh:`demo_${provider}_refresh_token`})}}));setErrors({});setAuthorized(true);setAuth(false)}
 function field(key:keyof Draft,label:string,placeholder:string,required=false,secret=false){return <label className="ld-field"><span>{label}{required?<b aria-hidden="true"> *</b>:<small>可选</small>}</span><input name={key} type={secret?'password':'text'} autoComplete="off" spellCheck={false} required={required} value={draft[key]} placeholder={placeholder} aria-invalid={!!errors[key]} aria-describedby={errors[key]?`ld-error-${key}`:undefined} onChange={e=>update(key,e.target.value)}/>{errors[key]&&<small id={`ld-error-${key}`} className="ld-error" role="alert">{errors[key]}</small>}</label>}
 return <div className="cd-overlay ld-overlay" onClick={onClose} onKeyDown={e=>{e.stopPropagation();if(e.key==='Escape'){if(auth)setAuth(false);else if(browsing)setBrowsing(false);else onClose()}if(e.key==='Tab'){const nodes=Array.from(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input,select,summary')||[]).filter(el=>el.getClientRects().length);const first=nodes[0],last=nodes[nodes.length-1];if(e.shiftKey&&(document.activeElement===first||document.activeElement===heading.current)){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}}}>
  <form ref={panel} className="ld-dialog" role="dialog" aria-modal="true" aria-labelledby="ld-title" noValidate onClick={e=>e.stopPropagation()} onSubmit={e=>{e.preventDefault();if(auth||browsing)return;if(step===1)setStep(2);else submit()}}>
   <header className="ld-header"><div className="ld-title-icon"><Link2 size={21}/></div><div><h2 id="ld-title" ref={heading} tabIndex={-1}>关联网盘</h2><p>让每一份文件，都有自己的位置。</p></div><button type="button" aria-label="关闭关联网盘" onClick={onClose}><X size={19}/></button></header>
   <div className="ld-steps"><span className={step===1?'active':'complete'}><i>{step===1?'1':<Check size={12}/>}</i>选择存储</span><ChevronRight size={13}/><span className={step===2?'active':''}><i>2</i>配置账号</span><span className="ld-demo-label">交互演示</span></div>
   <div className="ld-body">
    {step===1?<><h3>选择你要关联的存储</h3><p className="ld-description">不同网盘，使用各自的连接方式。</p><div className="ld-provider-grid">{providers.map(p=><button type="button" aria-pressed={provider===p.id} className={provider===p.id?'selected':''} key={p.id} onClick={()=>changeProvider(p.id)}><span className="ld-provider-icon" style={{background:p.color}}>{p.id==='nas'?<HardDrive size={23}/>:p.letter}</span><strong>{p.name}</strong><small>{p.description}</small>{provider===p.id&&<Check size={17}/>}</button>)}</div><div className="ld-note"><Cloud size={17}/><span>先体验完整的配置流程。此处不会连接网盘、读取本地目录或发送授权请求。</span></div></>:<>
     <div className="ld-provider-heading"><span className="ld-provider-icon" style={{background:current.color}}>{nas?<HardDrive size={22}/>:current.letter}</span><div><h3>{current.name}</h3><p>{current.description}</p></div><button type="button" onClick={()=>{setStep(1);setAuth(false);setBrowsing(false)}}>更换</button></div>
     {field('name','账号名称',`例如：${current.name} · 个人`,true)}
     <section className="ld-auth-section"><div className="ld-section-heading"><h4>{nas?'本地目录':'连接凭据'}</h4>{!nas&&<button type="button" onClick={()=>{setAuth(!auth);setBrowsing(false)}}>{provider==='quark'?<ScanLine size={15}/>:<WandSparkles size={15}/>} {provider==='quark'?'模拟扫码授权':'模拟获取 Token'}</button>}</div>
      {!nas&&<p className="ld-hint">请使用测试内容；凭据仅用于当前表单，不保存、不发送。</p>}
      {provider==='115'||provider==='baidu'?<div className="ld-field-grid">{field('access','访问令牌 access_token',provider==='115'?'输入访问令牌':'可留空，使用刷新令牌',provider==='115',true)}{field('refresh','刷新令牌 refresh_token','输入刷新令牌',true,true)}</div>:provider==='quark'?field('cookie','Cookie','输入测试 Cookie',true,true):<><label className="ld-field"><span>本地根目录<b aria-hidden="true"> *</b></span><div className="ld-path-input"><input aria-label="本地根目录" name="root" required value={draft.root} placeholder="例如：/mnt/media" aria-invalid={!!errors.root} aria-describedby={errors.root?'ld-error-root':undefined} onChange={e=>update('root',e.target.value)}/><button type="button" onClick={()=>{setBrowsing(!browsing);setDirectory(draft.root||'/mnt/media')}}><Folder size={15}/>浏览</button></div>{errors.root&&<small id="ld-error-root" className="ld-error" role="alert">{errors.root}</small>}</label><p className="ld-hint">对应部署服务的本地路径。浏览器中仅展示模拟目录。</p></>}
      {authorized&&<div className="ld-auth-success" role="status"><Check size={14}/>已填入演示凭据，可继续配置</div>}
      {auth&&<div className="ld-auth-preview"><div className="ld-auth-symbol">{provider==='quark'?<ScanLine size={34}/>:<Link2 size={30}/>}</div><strong>{provider==='quark'?'扫码授权演示':'获取 Token 演示'}</strong><p>{provider==='quark'?'真实接入后，此处展示授权二维码。':'真实接入后，此处打开对应网盘的授权流程。'}<br/>点击下方按钮模拟授权完成。</p><div><button type="button" onClick={()=>setAuth(false)}>取消</button><button type="button" className="cd-primary" onClick={simulateAuth}><Check size={14}/>模拟授权成功</button></div></div>}
      {browsing&&<div className="ld-directory-picker"><strong>选择演示目录</strong><small>以下为模拟服务器目录，不访问你的设备。</small>{['/mnt/media','/mnt/downloads','/mnt/backup','/data/shared'].map(path=><button type="button" key={path} aria-pressed={directory===path} onClick={()=>setDirectory(path)}><Folder size={17}/>{path}{directory===path&&<Check size={15}/>}</button>)}<div><button type="button" onClick={()=>setBrowsing(false)}>取消</button><button type="button" className="cd-primary" onClick={()=>{update('root',directory);setBrowsing(false)}}>选择此目录</button></div></div>}
     </section>
     <details className="ld-advanced" open={advanced} onToggle={e=>setAdvanced(e.currentTarget.open)}><summary>存储选项 <span>{nas?'缓存设置':'下载、删除与目录设置'}</span><ChevronRight size={14}/></summary><div className="ld-field-grid">{!nas&&<><label className="ld-field"><span>下载模式</span><select value={draft.download} onChange={e=>update('download',e.target.value)}><option value="redirect">302 重定向</option><option value="proxy">本机代理</option></select></label><label className="ld-field"><span>删除模式</span><select value={draft.deletion} onChange={e=>update('deletion',e.target.value)}><option value="trash">移到回收站</option><option value="permanent">直接删除</option></select></label>{field('root',provider==='baidu'?'根目录路径':'根目录 ID',provider==='baidu'?'/':'0',true)}</>}<label className="ld-field"><span>缓存时间 <small>分钟</small></span><input name="cache" type="number" min="0" max="10080" step="1" value={draft.cache} aria-invalid={!!errors.cache} aria-describedby={errors.cache?'ld-error-cache':undefined} onChange={e=>update('cache',e.target.value)}/>{errors.cache&&<small id="ld-error-cache" className="ld-error" role="alert">{errors.cache}</small>}</label></div><p className="ld-hint">{nas?'默认不缓存目录。':'默认缓存 30 分钟。'}设为 0 时关闭缓存；这些设置仅用于演示。</p></details>
     <div className="ld-note"><Cloud size={16}/><span>仅保存账号名称与存储选项。Token 和 Cookie 在关闭后清除。</span></div>
     {errors.form&&<p className="ld-error" role="alert">{errors.form}</p>}
    </>}
   </div>
   <footer className="ld-footer"><button type="button" onClick={()=>{if(step===2){setStep(1);setAuth(false);setBrowsing(false)}else onClose()}}>{step===2&&<ArrowLeft size={15}/>} {step===1?'取消':'上一步'}</button><button type="submit" className="cd-primary" disabled={step===2&&(auth||browsing)}>{step===1?'下一步':'添加演示账号'}{step===1?<ChevronRight size={15}/>:<Check size={15}/>}</button></footer>
  </form>
 </div>
}
