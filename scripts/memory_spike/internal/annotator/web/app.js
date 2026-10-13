// 文件用途：呈现冻结原话并收集真人判断；显式保存到本机服务，不生成或预填事实。
"use strict";
/** 查找固定页面元素。 */
const $ = (id) => document.getElementById(id);
let sample, state, token, dirty = false, busy = false, selectedQuote = "";
const categories = {auto:"值得长期记住",temporary:"临时事项",sensitive:"敏感信息"};
const stability = ["用户明确自述","多次出现的行为信号","单次弱信号"];

/** 获取当前题目的草稿；未操作题不自动视为已审阅。 */
function currentMark() { return state.marks[sample.messages[state.current].id] || {status:"", entries:[]}; }
/** 仅已明确跳过或填齐事实的题目计入完成数量。 */
function isDone(mark) { return mark?.status === "skip" || (mark?.status === "marked" && mark.entries.length > 0 && mark.entries.every(g => g.fact.trim() && g.stability)); }
/** 更新当前题草稿并撤销旧完成标志，保存前提示不离开。 */
function changed(mark) { state.marks[sample.messages[state.current].id] = mark; state.annotation.complete = false; dirty = true; $("success").hidden = true; $("save-status").textContent = "有未保存的修改"; progress(); }
/** 显示错误而不清除用户正在编辑的内容。 */
function error(message) { $("error").textContent = message; $("error").hidden = !message; }
/** 生成纯文本 DOM，防止聊天原文或输入被作为 HTML 执行。 */
function element(tag, text, className) { const node = document.createElement(tag); if (text !== undefined) node.textContent = text; if (className) node.className = className; return node; }
/** 汇总进度与可回到的题号，不把“拿不准”当作完成。 */
function progress() {
 const done = sample.messages.filter(m => isDone(state.marks[m.id])).length;
 const uncertain = Object.values(state.marks).filter(m => m.status === "uncertain").length;
 $("done").textContent = done; $("total").textContent = ` / ${sample.messages.length} 条`;
 $("progress").max = sample.messages.length; $("progress").value = done;
 $("remaining").textContent = `还剩 ${sample.messages.length-done} 条${uncertain ? ` · ${uncertain} 条拿不准` : ""}`;
 $("questions").replaceChildren();
 sample.messages.forEach((message, index) => {
  const completed = isDone(state.marks[message.id]);
  const button = element("button", `${index+1}${completed ? " ✓" : ""}`);
  button.dataset.done = completed; button.setAttribute("aria-current", String(index===state.current));
  button.setAttribute("aria-label", `第 ${index+1} 条，${completed ? "已完成" : "未完成"}`);
  button.onclick = () => navigate(index); $("questions").append(button);
 });
}
/** 构建带占位提示的选择框，所有判断均由用户自己选择。 */
function selectField(labelText, options, selected, onChange) {
 const label = element("label",labelText), select = element("select");
 const blank = element("option","请选择"); blank.value=""; select.append(blank);
 for (const [value,text] of Object.entries(options)) { const option=element("option",text); option.value=value; select.append(option); }
 select.value=selected; select.onchange=()=>onChange(select.value); label.append(select); return label;
}
/** 渲染多条事实的独立编辑区；一段话允许同时含稳定、临时与敏感信息。 */
function renderFacts() {
 const mark=currentMark(); $("facts-panel").hidden=mark.status!=="marked"; $("facts").replaceChildren();
 mark.entries.forEach((g,index)=>{
  const section=element("div",undefined,"fact"), label=element("label",`事实 ${index+1} · 一条只表达一件事`), text=element("textarea");
  text.id=`fact-${index}`; label.htmlFor=text.id; text.value=g.fact; text.placeholder="划选原话后添加，或在这里用一句话填写";
  text.oninput=()=>{ g.fact=text.value; changed(mark); };
  const fields=element("div",undefined,"fact-fields");
  fields.append(selectField("这条信息的类别",categories,g.category,value=>{ if(value) {g.category=value; changed(mark); renderChoices();} else {renderFacts();} }));
  fields.append(selectField("你根据什么判断？",Object.fromEntries(stability.map(x=>[x,x])),g.stability,value=>{ g.stability=value; changed(mark); }));
  const remove=element("button","删除这条","quiet remove"); remove.onclick=()=>{ mark.entries.splice(index,1); changed(mark); renderFacts(); };
  fields.append(remove); section.append(label,text,fields); $("facts").append(section);
 });
}
/** 将题目状态反映到按钮；包含多个类别时同时显示已选类别。 */
function renderChoices() {
 const mark=currentMark();
 document.querySelectorAll("[data-choice]").forEach(button=>button.setAttribute("aria-pressed",String(mark.status===button.dataset.choice || (mark.status==="marked" && mark.entries.some(g=>g.category===button.dataset.choice)))));
}
/** 切换题目时展示全文和同会话上下文，不使用模型提示影响独立标注。 */
function render() {
 selectedQuote="";
 const message=sample.messages[state.current];
 $("position").textContent=`第 ${state.current+1} / ${sample.messages.length} 条`;
 $("metadata").textContent=`${message.date.slice(0,10)} · 聊天 ${message.session.replace("s_","")}`;
 $("message").textContent=message.text;
 $("previous").disabled=state.current===0;
 $("next").textContent=state.current===sample.messages.length-1 ? "保存并检查进度 →" : "保存，下一条 →";
 $("context").open=false; $("context-content").replaceChildren();
 const neighbors=sample.messages.filter(m=>m.session===message.session && m.id!==message.id);
 $("context").hidden=neighbors.length===0;
 neighbors.forEach(m=>$("context-content").append(element("p",`${m.date.slice(0,10)}\n${m.text}`)));
 $("success").hidden=!state.annotation.complete;
 renderChoices(); renderFacts(); progress();
}
/** 选择类别只创建空白输入；切换为跳过前避免误删已填写的事实。 */
function choose(choice) {
 if(busy) return;
 const mark=currentMark();
 if(choice==="skip" || choice==="uncertain") {
  if(mark.entries.some(g=>g.fact.trim()) && !confirm("切换后将清除此题已填写的事实，继续吗？")) return;
  mark.status=choice; mark.entries=[];
 } else {
  mark.status="marked";
  if(!mark.entries.length) mark.entries.push({id:"",fact:"",category:choice,evidence:[sample.messages[state.current].id],stability:""});
  else if(mark.entries.length===1) mark.entries[0].category=choice;
  else { error("这段话已有多条事实，请在每条事实下面分别选择类别。"); return; }
 }
 changed(mark); error(""); renderChoices(); renderFacts();
}
/** 保存时锁定工作区；成功才更新版本，失败保持本地输入并提供原错误。 */
async function save(complete=false, destination=state.current) {
 if(busy) return false;
 busy=true; error(""); const payload=structuredClone(state); payload.annotation.complete=complete; payload.current=destination;
 const controls=[...document.querySelectorAll("button,textarea,select")]; const disabled=controls.map(node=>node.disabled);
 controls.forEach(node=>node.disabled=true); $("workspace").setAttribute("aria-busy","true");
 try {
  const response=await fetch("/api/state",{method:"POST",headers:{"Content-Type":"application/json","X-Annotation-Token":token},body:JSON.stringify(payload)});
  if(!response.ok) throw new Error(await response.text());
  state=await response.json(); dirty=false;
  $("save-status").textContent=`已保存 ${new Date(state.saved_at).toLocaleTimeString("zh-CN",{hour:"2-digit",minute:"2-digit"})}`;
  return true;
 } catch(e) { error(`未保存成功：${e.message}。请保留此页，检查后再点保存。`); return false; }
 finally { busy=false; controls.forEach((node,i)=>node.disabled=disabled[i]); $("workspace").setAttribute("aria-busy","false"); }
}
/** 切题前持久化当前草稿和目标题号，失败时留在原题。 */
async function navigate(index) { if(index<0 || index>=sample.messages.length || busy) return; if(await save(state.annotation.complete,index)) { render(); $("heading").focus({preventScroll:true}); window.scrollTo({top:0,behavior:"instant"}); } }
/** 追加用户选中的原句，空白第一条优先复用，不自动选择稳定性。 */
function addFact(useSelection) {
 const quote=useSelection ? selectedQuote : "";
 if(useSelection && !quote) { error("请先用鼠标或长按，在上面的原话里选中要标记的句子。"); return; }
 const mark=currentMark(), blank=mark.entries.find(g=>!g.fact.trim());
 if(blank && useSelection) blank.fact=quote;
 else mark.entries.push({id:"",fact:quote,category:mark.entries.at(-1)?.category || "auto",evidence:[sample.messages[state.current].id],stability:""});
 changed(mark); error(""); renderFacts();
}
/** 找到下一道尚未完整确认的题目；全部完成时仍需用户明确提交。 */
async function goPending() {
 const index=sample.messages.findIndex(m=>!isDone(state.marks[m.id]));
 if(index<0) { error("每一条都已检查，可以点击左侧「完成并保存标注」。"); return; }
 await navigate(index);
}
/** 从本机加载并绑定操作；连接失败不显示伪造样本或可误点的空题。 */
async function start() {
 try {
  const response=await fetch("/api/state"); if(!response.ok) throw new Error("无法读取本机样本");
  ({sample,state,token}=await response.json()); render();
  $("save-status").textContent=state.saved_at ? "已恢复上次进度" : "尚未开始";
  document.querySelectorAll("[data-choice]").forEach(button=>button.onclick=()=>choose(button.dataset.choice));
  $("previous").onclick=()=>navigate(state.current-1);
  $("next").onclick=async()=>{ if(state.current<sample.messages.length-1) await navigate(state.current+1); else if(await save()) await goPending(); };
  $("save").onclick=async()=>{if(await save(state.annotation.complete)) render();};
  $("pending").onclick=goPending;
  $("finish").onclick=async()=>{if(await save(true)) {render(); $("success").scrollIntoView({block:"start"});} else $("error").scrollIntoView({block:"center"});};
  $("edit").onclick=()=>{ $("success").hidden=true; $("heading").focus(); };
  $("add-selection").onclick=()=>addFact(true); $("add-empty").onclick=()=>addFact(false);
  // 按钮获焦可能清除选区；仅缓存当前原话内部选择过的原文，换题时清空。
  document.addEventListener("selectionchange",()=>{
   const selection=window.getSelection(), source=$("message"), quote=selection?.toString().trim();
   if(quote && (source.contains(selection.anchorNode)||source.contains(selection.focusNode)) && source.textContent.includes(quote)) selectedQuote=quote;
  });
  window.addEventListener("keydown",event=>{if(!event.ctrlKey&&!event.metaKey&&!event.altKey&&!/INPUT|TEXTAREA|SELECT/.test(event.target.tagName)&&/^[1-5]$/.test(event.key)){event.preventDefault();choose(["skip","auto","temporary","sensitive","uncertain"][Number(event.key)-1]);}});
  window.addEventListener("beforeunload",event=>{if(dirty||busy){event.preventDefault();event.returnValue="";}});
 } catch(e) { error(`页面未加载：${e.message}。请确认本机标注服务正在运行，再刷新。`); $("question-body").hidden=true; $("finish").disabled=true; $("pending").disabled=true; }
 finally { $("workspace").setAttribute("aria-busy","false"); }
}
start();
