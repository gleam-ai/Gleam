package agent

import (
	"fmt"
	"strings"

	"gleam/pkg/types"
)

func applyAcceptanceVerdict(res *types.GoalResult, acceptance []string) {
	if res == nil || res.Status != types.GoalSuccess {
		return
	}
	if bad := failedArtifacts(verifyArtifacts(res.Steps)); len(bad) > 0 {
		res.Status = types.GoalPartial
		if res.Error == "" {
			res.Error = artifactFailureNote(bad) + "。步骤自述成功，但代码实测对不上。"
		}
		return
	}
	if len(acceptance) == 0 {
		return
	}
	if len(res.Checks) == 0 {
		res.Status = types.GoalPartial
		if res.Error == "" {
			res.Error = fmt.Sprintf("验收标准（%d 条）未能核对：反思器不可用或输出无法解析，无法确认是否达标",
				len(acceptance))
		}
		return
	}
	missed := missedCriteria(res.Checks)
	if len(missed) == 0 {
		return
	}
	if countPassed(res.Checks) == 0 {
		res.Status = types.GoalFailed
	} else {
		res.Status = types.GoalPartial
	}
	if res.Error == "" {
		res.Error = fmt.Sprintf("验收未全部通过（%d/%d），未达标：%s",
			countPassed(res.Checks), len(res.Checks), strings.Join(missed, "；"))
	}
}
