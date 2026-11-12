package agent

import (
	"strings"
	"testing"
)

func TestNarratorStyles(t *testing.T) {
	tests := []struct {
		name   string
		style  string
		expect map[string]string // method -> expected substring
	}{
		{
			name:  "efficient",
			style: "efficient",
			expect: map[string]string{
				"PlanStart":    "收到，先拆解一下。",
				"PlanDone":     "开干",
				"Compressed":   "省 token",
				"ReflectStart": "检查一下",
				"Replan":       "差一点",
			},
		},
		{
			name:  "gentle",
			style: "gentle",
			expect: map[string]string{
				"PlanStart":    "好，交给我。",
				"PlanDone":     "开始咯",
				"Compressed":   "不会忘记",
				"ReflectStart": "我来看看做得怎么样",
				"Replan":       "我再想办法补一补",
			},
		},
		{
			name:  "rigorous",
			style: "rigorous",
			expect: map[string]string{
				"PlanStart":    "约束与步骤",
				"PlanDone":     "依赖与参数均已校验",
				"Compressed":   "关键信息保留",
				"ReflectStart": "逐项核对",
				"Replan":       "进入重规划并携带失败反馈",
			},
		},
		{
			name:   "invalid_falls_back_to_efficient",
			style:  "unknown",
			expect: map[string]string{"PlanStart": "收到，先拆解一下。"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := Narrator{Style: tt.style}

			if got := n.PlanStart(); !strings.Contains(got, tt.expect["PlanStart"]) {
				t.Errorf("PlanStart() = %q, 包含 %q", got, tt.expect["PlanStart"])
			}
			if got := n.Compressed(); !strings.Contains(got, tt.expect["Compressed"]) {
				t.Errorf("Compressed() = %q, 包含 %q", got, tt.expect["Compressed"])
			}
			if got := n.ReflectStart(); !strings.Contains(got, tt.expect["ReflectStart"]) {
				t.Errorf("ReflectStart() = %q, 包含 %q", got, tt.expect["ReflectStart"])
			}
		})
	}
}

func TestNarratorPlanDone(t *testing.T) {
	nEfficient := Narrator{Style: "efficient"}
	nGentle := Narrator{Style: "gentle"}
	nRigorous := Narrator{Style: "rigorous"}

	steps := []int{1, 5, 10, 100}

	for _, steps := range steps {
		t.Run(string(rune('0'+steps%10)), func(t *testing.T) {
			gotEff := nEfficient.PlanDone(steps)
			if !strings.Contains(gotEff, "步") || !strings.Contains(gotEff, "开干") {
				t.Errorf("efficient PlanDone(%d) = %q, 应包含数字和'开干'", steps, gotEff)
			}

			gotGentle := nGentle.PlanDone(steps)
			if !strings.Contains(gotGentle, "步") || !strings.Contains(gotGentle, "想好了") {
				t.Errorf("gentle PlanDone(%d) = %q, 应包含'想好了'", steps, gotGentle)
			}

			gotRigorous := nRigorous.PlanDone(steps)
			if !strings.Contains(gotRigorous, "步") || !strings.Contains(gotRigorous, "校验") {
				t.Errorf("rigorous PlanDone(%d) = %q, 应包含'校验'", steps, gotRigorous)
			}
		})
	}
}

func TestNarratorReplScore(t *testing.T) {
	nEfficient := Narrator{Style: "efficient"}
	nGentle := Narrator{Style: "gentle"}
	nRigorous := Narrator{Style: "rigorous"}

	scores := []int{0, 50, 85, 100}

	for _, score := range scores {
		t.Run(string(rune('0'+score%10)), func(t *testing.T) {
			gotEff := nEfficient.Replan(score)
			if !strings.Contains(gotEff, string(rune('0'+score%10))) || !strings.Contains(gotEff, "差一点") {
				t.Errorf("efficient Replan(%d) = %q, 应包含完成度和'差一点'", score, gotEff)
			}

			gotGentle := nGentle.Replan(score)
			if !strings.Contains(gotGentle, string(rune('0'+score%10))) || !strings.Contains(gotGentle, "还有差距") {
				t.Errorf("gentle Replan(%d) = %q, 应包含'还有差距'", score, gotGentle)
			}

			gotRigorous := nRigorous.Replan(score)
			if !strings.Contains(gotRigorous, string(rune('0'+score%10))) || !strings.Contains(gotRigorous, "未达标") {
				t.Errorf("rigorous Replan(%d) = %q, 应包含'未达标'", score, gotRigorous)
			}
		})
	}
}
