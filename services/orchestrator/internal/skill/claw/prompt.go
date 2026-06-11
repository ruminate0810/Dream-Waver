package claw

// nextStepPrompt nudges a sub-agent at each step boundary. The real per-role
// discipline lives in roles.go (Role.SystemPrompt) + each tool's description;
// this is a short shared reminder appended to every sub-agent loop.
const nextStepPrompt = `推进你的子任务:用你的工具完成它,完成后用一段中文小结成果,然后调用 terminate 结束。`
