package agent

import "fmt"

type Narrator struct {
	Style string
}

func (n Narrator) style() string {
	switch n.Style {
	case "gentle", "rigorous":
		return n.Style
	default:
		return "efficient"
	}
}

func (n Narrator) PlanStart() string {
	switch n.style() {
	case "gentle":
		return "好，交给我。先想清楚怎么做……"
	case "rigorous":
		return "目标已明确，开始拆解约束与步骤。"
	default:
		return "收到，先拆解一下。"
	}
}

func (n Narrator) PlanDone(steps int) string {
	switch n.style() {
	case "gentle":
		return fmt.Sprintf("想好了，一共 %d 步，我开始咯。", steps)
	case "rigorous":
		return fmt.Sprintf("已拆解为 %d 步，依赖与参数均已校验，开始执行。", steps)
	default:
		return fmt.Sprintf("%d 步，开干。", steps)
	}
}

func (n Narrator) Compressed() string {
	switch n.style() {
	case "gentle":
		return "顺手把早期对话整理成了摘要，不会忘记之前聊过的。"
	case "rigorous":
		return "早期上下文已压缩为滚动摘要，关键信息保留。"
	default:
		return "早期上下文已压缩，省 token 不丢上下文。"
	}
}

func (n Narrator) ReflectStart() string {
	switch n.style() {
	case "gentle":
		return "我来看看做得怎么样……"
	case "rigorous":
		return "正在逐项核对执行结果与目标差距。"
	default:
		return "检查一下成果。"
	}
}

func (n Narrator) Replan(score int) string {
	switch n.style() {
	case "gentle":
		return fmt.Sprintf("完成度 %d/100，还有差距，我再想办法补一补。", score)
	case "rigorous":
		return fmt.Sprintf("完成度 %d/100，未达标，进入重规划并携带失败反馈。", score)
	default:
		return fmt.Sprintf("完成度 %d/100，差一点，换个打法再来。", score)
	}
}
