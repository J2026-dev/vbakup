let state={nodes:[],repositories:[],tasks:[],backups:[]};
const $=selector=>document.querySelector(selector);
const escapeHTML=value=>String(value??'').replace(/[&<>'"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));
const formatDate=value=>value?new Intl.DateTimeFormat('zh-CN',{dateStyle:'short',timeStyle:'short'}).format(new Date(value)):'从未';
const size=value=>{if(!value)return '0 B';const units=['B','KB','MB','GB','TB'];const i=Math.min(Math.floor(Math.log(value)/Math.log(1024)),4);return `${(value/1024**i).toFixed(i?1:0)} ${units[i]}`};
const nameOf=(items,id)=>items.find(item=>item.id===id)?.name||id;
function toast(message){const el=$('#toast');el.textContent=message;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2600)}
async function api(url,options={}){const response=await fetch(url,{headers:{'Content-Type':'application/json'},...options});const data=await response.json().catch(()=>({}));if(!response.ok)throw new Error(data.error||response.statusText);return data}
async function load(){state=await api('/api/state');render()}
function table(headers,rows){if(!rows.length)return '<div class="empty">暂无数据</div>';return `<table><thead><tr>${headers.map(x=>`<th>${x}</th>`).join('')}</tr></thead><tbody>${rows.join('')}</tbody></table>`}
function render(){
  $('#online-count').textContent=state.nodes.filter(n=>n.status==='online').length;$('#task-count').textContent=state.tasks.length;$('#backup-count').textContent=state.backups.length;$('#repo-count').textContent=state.repositories.length;
  $('#install-command').textContent=state.install_command;
  $('#node-list').innerHTML=table(['节点','状态','系统','已发现服务','最近心跳'],state.nodes.map(n=>`<tr><td><strong>${escapeHTML(n.name)}</strong><br><small>${escapeHTML(n.id)}</small></td><td><span class="status ${n.status}">${n.status==='online'?'在线':'离线'}</span></td><td>${escapeHTML(n.os)} / ${escapeHTML(n.architecture)}</td><td class="services">${escapeHTML((n.services||[]).join('、')||'等待扫描')}</td><td>${formatDate(n.last_seen)}</td></tr>`));
  $('#task-list').innerHTML=table(['计划','节点','频率','状态','上次执行','操作'],state.tasks.map(t=>`<tr><td><strong>${escapeHTML(t.name)}</strong></td><td>${escapeHTML(nameOf(state.nodes,t.node_id))}</td><td>${scheduleName(t.schedule)}</td><td><span class="status ${String(t.last_status).startsWith('failed')?'failed':t.last_status==='success'?'success':''}">${escapeHTML(t.last_status||'等待')}</span></td><td>${formatDate(t.last_run)}</td><td><button class="secondary run-task" data-id="${t.id}">立即备份</button></td></tr>`));
  $('#backup-list').innerHTML=table(['备份时间','来源节点','服务','大小','校验','操作'],state.backups.map(b=>`<tr><td>${formatDate(b.created_at)}</td><td>${escapeHTML(nameOf(state.nodes,b.node_id))}</td><td class="services">${escapeHTML((b.services||[]).join('、'))}</td><td>${size(b.size)}</td><td><code>${escapeHTML(b.sha256.slice(0,12))}…</code></td><td><button class="secondary restore" data-id="${b.id}">恢复</button></td></tr>`));
  $('#repo-list').innerHTML=table(['名称','WebDAV 地址','用户','基础目录'],state.repositories.map(r=>`<tr><td><strong>${escapeHTML(r.Name||r.name)}</strong></td><td>${escapeHTML(r.URL||r.url)}</td><td>${escapeHTML(r.Username||r.username||'-')}</td><td>${escapeHTML(r.BasePath||r.base_path||'/')}</td></tr>`));
  const nodeOptions='<option value="">请选择节点</option>'+state.nodes.map(n=>`<option value="${n.id}">${escapeHTML(n.name)} (${n.status==='online'?'在线':'离线'})</option>`).join('');
  $('#task-form [name=node_id]').innerHTML=nodeOptions;$('#restore-form [name=node_id]').innerHTML=nodeOptions;
  $('#task-form [name=repository_id]').innerHTML='<option value="">请选择备份空间</option>'+state.repositories.map(r=>`<option value="${r.ID||r.id}">${escapeHTML(r.Name||r.name)}</option>`).join('');
}
function scheduleName(value){return {'@hourly':'每小时','@6hours':'每 6 小时','@12hours':'每 12 小时','@daily':'每天','@weekly':'每周'}[value]||value}
document.addEventListener('click',async event=>{const tab=event.target.closest('[data-view]');if(tab){document.querySelectorAll('.tabs button,.view').forEach(x=>x.classList.remove('active'));tab.classList.add('active');$('#'+tab.dataset.view).classList.add('active')}
  const run=event.target.closest('.run-task');if(run){try{await api(`/api/tasks/${run.dataset.id}/run`,{method:'POST'});toast('备份任务已下发');await load()}catch(e){toast(e.message)}}
  const restore=event.target.closest('.restore');if(restore){$('#restore-form [name=backup_id]').value=restore.dataset.id;$('#restore-dialog').showModal()}
});
$('#refresh').onclick=()=>load().catch(e=>toast(e.message));$('#show-install').onclick=()=>$('#install-dialog').showModal();$('#show-task').onclick=()=>$('#task-dialog').showModal();$('#show-repo').onclick=()=>$('#repo-dialog').showModal();
document.querySelectorAll('dialog .close').forEach(button=>button.onclick=()=>button.closest('dialog').close());
$('#copy-install').onclick=async()=>{await navigator.clipboard.writeText(state.install_command);toast('安装命令已复制')};
$('#repo-form').onsubmit=async event=>{event.preventDefault();try{await api('/api/repositories',{method:'POST',body:JSON.stringify(Object.fromEntries(new FormData(event.target)))});event.target.reset();$('#repo-dialog').close();toast('备份空间已保存');await load()}catch(e){toast(e.message)}};
$('#task-form').onsubmit=async event=>{event.preventDefault();const data=Object.fromEntries(new FormData(event.target));data.paths=data.paths.split('\n').map(x=>x.trim()).filter(Boolean);data.include_docker=event.target.include_docker.checked;data.include_databases=event.target.include_databases.checked;try{await api('/api/tasks',{method:'POST',body:JSON.stringify(data)});event.target.reset();$('#task-dialog').close();toast('备份计划已创建');await load()}catch(e){toast(e.message)}};
$('#restore-form').onsubmit=async event=>{event.preventDefault();const data=Object.fromEntries(new FormData(event.target));data.confirm=event.target.confirm.checked;try{await api(`/api/backups/${data.backup_id}/restore`,{method:'POST',body:JSON.stringify({node_id:data.node_id,confirm:data.confirm})});event.target.reset();$('#restore-dialog').close();toast('恢复任务已下发')}catch(e){toast(e.message)}};
load().catch(e=>toast(e.message));
