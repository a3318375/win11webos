export type Entry = { id:string; name:string; parent:string; kind:'folder'|'text'|'image'; content?:string; src?:string; deleted?:boolean };
export type FileClipboard = { mode:'copy'|'cut'; sourceId:string; items:Entry[] };
export function validName(name:string) {
  if (!name.trim()) return '名称不能为空。';
  if (/[\\/:*?"<>|]/.test(name)) return '名称不能包含以下字符：\\ / : * ? " < > |';
  if (/[. ]$/.test(name) || /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\.|$)/i.test(name)) return '此名称不可用，请使用其他名称。';
  return '';
}
export function uniqueName(entries:Entry[], parent:string, name:string) {
  const used = new Set(entries.filter(e=>e.parent===parent&&!e.deleted).map(e=>e.name.toLocaleLowerCase()));
  if (!used.has(name.toLocaleLowerCase())) return name;
  const dot=name.lastIndexOf('.'), stem=dot>0?name.slice(0,dot):name, ext=dot>0?name.slice(dot):'';
  for(let i=2;;i++){const candidate=`${stem} (${i})${ext}`;if(!used.has(candidate.toLocaleLowerCase()))return candidate;}
}
export function snapshot(entries:Entry[], id:string):Entry[] {
  const root=entries.find(e=>e.id===id&&!e.deleted);if(!root)return [];
  return [{...root},...entries.filter(e=>e.parent===id&&!e.deleted).flatMap(e=>snapshot(entries,e.id))];
}
export function moveEntry(entries:Entry[],id:string,destination:string):Entry[] {
  const source=entries.find(e=>e.id===id&&!e.deleted);
  if(!source)throw Error('源文件已不存在。');
  if(/^f[0-5]$/.test(id))throw Error('无法移动系统文件夹。');
  if(destination!=='root'&&!entries.some(e=>e.id===destination&&e.kind==='folder'&&!e.deleted))throw Error('目标文件夹不存在。');
  let cursor:string|undefined=destination;const visited=new Set<string>();
  while(cursor){if(cursor===id||visited.has(cursor))throw Error('不能将文件夹移动到其自身或子文件夹中。');visited.add(cursor);cursor=entries.find(e=>e.id===cursor)?.parent;}
  if(source.parent===destination)return entries;
  return entries.map(e=>e.id===id?{...e,parent:destination,name:uniqueName(entries,destination,e.name)}:e);
}
export function pasteEntries(entries:Entry[],clipboard:FileClipboard,destination:string):{entries:Entry[];id:string} {
  if(clipboard.mode==='cut')return {entries:moveEntry(entries,clipboard.sourceId,destination),id:clipboard.sourceId};
  if(destination!=='root'&&!entries.some(e=>e.id===destination&&e.kind==='folder'&&!e.deleted))throw Error('目标文件夹不存在。');
  const root=clipboard.items[0];if(!root)throw Error('剪贴板为空。');
  const ids=new Map(clipboard.items.map(e=>[e.id,crypto.randomUUID()]));
  const copies=clipboard.items.map(e=>({...e,id:ids.get(e.id)!,parent:e.id===root.id?destination:ids.get(e.parent)!,name:e.id===root.id?uniqueName(entries,destination,e.name):e.name,deleted:false}));
  return {entries:[...entries,...copies],id:ids.get(root.id)!};
}
