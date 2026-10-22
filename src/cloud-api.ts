export type ProviderId = '115'|'baidu'|'quark'|'nas';
export type MountAccount = {id:string;name:string;provider:ProviderId;config:{root:string;cacheMinutes:number;downloadMode:string};createdAt:string};
export type MountInput = {name:string;provider:ProviderId;config:MountAccount['config'];credentials:{accessToken:string;refreshToken:string;cookie:string};authSession?:string};
export type MountFile = {id:string;name:string;directory:boolean;size:number;modifiedAt:string;kind:'folder'|'text'|'image'|'video'|'audio'|'file'};
export type FileListing = {items:MountFile[];cacheHit:boolean;fetchedAt:string;total:number};
export type AuthState = {id:string;status:'pending'|'success'|'failed'|'expired';url?:string;qr?:string;expiresAt:string;message?:string};
export type MountProvider = {id:ProviderId;name:string;automaticAuth:boolean;authHint:string};
export type Directory = {name:string;path:string};
export type TransferInput={sourceAccount:string;sourceParent:string;sourceTrail:string[];ids:string[];targetAccount:string;targetParent:string;conflict:'skip'|'rename'};
export type TransferTask={id:string;name:string;sourceName:string;targetName:string;targetAccount:string;targetParent:string;state:'queued'|'running'|'paused'|'completed'|'failed'|'canceled';phase:string;currentFile:string;totalFiles:number;completedFiles:number;skippedFiles:number;totalBytes:number;processedBytes:number;speed:number;error:string;createdAt:string;updatedAt:string};
export async function apiRequest<T>(path:string,method='GET',body?:unknown,signal?:AbortSignal):Promise<T>{
 let response:Response;try{response=await fetch('/api/v1'+path,{method,signal,headers:body===undefined?undefined:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});}catch(error){if((error as Error).name==='AbortError')throw error;throw new Error('无法连接挂载服务，请确认后端已启动');}
 let data:{data:T;error?:{message:string}};try{data=await response.json()}catch{throw new Error('后端暂时不可用，请检查服务状态');}
 if(!response.ok||data.error)throw new Error(data.error?.message||`请求失败（${response.status}）`);return data.data;
}
export const cloudApi={
 transfers:(signal?:AbortSignal)=>apiRequest<TransferTask[]>('/transfers','GET',undefined,signal),
 createTransfer:(input:TransferInput)=>apiRequest<TransferTask>('/transfers','POST',input),
 transferAction:(id:string,action:'pause'|'cancel'|'retry'|'remove')=>apiRequest<TransferTask>(`/transfers/${encodeURIComponent(id)}/action`,'POST',{action}),
 action:(id:string,input:{action:string;parent:string;sourceParent:string;ids:string[];name?:string})=>apiRequest<{completed:number}>(`/accounts/${encodeURIComponent(id)}/actions`,'POST',input),
 accounts:(signal?:AbortSignal)=>apiRequest<MountAccount[]>('/accounts','GET',undefined,signal), providers:()=>apiRequest<MountProvider[]>('/providers'),
 test:(input:MountInput)=>apiRequest<{status:string;count:number;message:string;auth:AuthState|null}>('/accounts/test','POST',input),create:(input:MountInput)=>apiRequest<MountAccount>('/accounts','POST',input),remove:(id:string)=>apiRequest('/accounts/'+encodeURIComponent(id),'DELETE'),
 files:(id:string,parent:string,refresh=false,signal?:AbortSignal)=>apiRequest<FileListing>(`/accounts/${encodeURIComponent(id)}/files?${new URLSearchParams({parent,refresh:String(refresh)})}`,'GET',undefined,signal),
 directories:(path='')=>apiRequest<Directory[]>('/local/directories?'+new URLSearchParams({path})),startAuth:(provider:ProviderId)=>apiRequest<AuthState>('/auth/sessions','POST',{provider}),pollAuth:(id:string,signal?:AbortSignal)=>apiRequest<AuthState>('/auth/sessions/'+encodeURIComponent(id),'GET',undefined,signal),cancelAuth:(id:string)=>apiRequest('/auth/sessions/'+encodeURIComponent(id),'DELETE'),
};
