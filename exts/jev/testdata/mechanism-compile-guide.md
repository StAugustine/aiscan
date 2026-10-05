# Reflex 编译实验引导

你只编译可复用程序，不继续原任务。先从证据中确定参数、效果、只读操作、成功证据及未知结果的恢复方式，然后输出原要求的 JSON。

方法：一次列出所有缺参；原生工具名称及参数形状从 tools 取得，command 内协议从 commands 取得；所有任务身份由 args 或当前结果提供。效果 read:false，轮询 read:true。HTTP 503 或工具错误都不能证明效果未发生。先查询已有对象；没有查询能力时 defer，不重新提交。report 必须包含当前实际结果。

完整结构示例（仅说明方法；按当前提供的原生命令和真实字段编译，不复制示例命令）：

```javascript
function(context,args){
  if(!args || !args.target || !args.value)
    return {defer:'missing current arguments',parameters:'target, value'};
  const started=execute({name:'documented-tool',arguments:{target:args.target,value:args.value},read:false,step:'submit',occurrence:0});
  for(let i=0;i<8;i++){
    const current=execute({name:'documented-status-tool',arguments:{target:args.target},read:true});
    if(current.is_error)return {defer:'status unavailable; preserve prior effect'};
    if(current.data && current.data.complete && current.data.target===args.target && current.data.receipt)
      return {report:current.data};
  }
  return {defer:'still pending'};
}
```

示例的 artifact 还必须声明 `steps.submit`，包含当前 `native_contracts` 中的契约 ID 和 `count:1`。效果身份不可省略。运行时已自动加载 `exts/jev/skills/reflex-compiler/SKILL.md`，提供 inspect_evidence、完整验收与连续修复流程；本文件只用于引导对照实验。

修复示例：轮询一直返回第一次 pending → 检查读标记；样本参数改变后仍绑定旧对象 → 移除源码常量；shell exit=0 但业务未完成 → 检查真实业务终态及回执；恢复重复写入 → 查询历史和当前状态。不要把样本答案、历史回执或测试预期写入程序。诊断指出机制无法承载的操作时返回明确 defer，不改 read 标记绕过保护。
