package skills

import "mobilevc/internal/protocol"

type Definition struct {
	Name        string
	Description string
	Prompt      string
	ResultView  string
	TargetType  string
}

func Builtins() map[string]Definition {
	return map[string]Definition{
		"review": {
			Name:        "review",
			Description: "审查当前 diff",
			Prompt:      "请审查下面这份 diff，重点指出 bug、可维护性问题、回归风险和可执行改进建议。",
			ResultView:  "review-card",
			TargetType:  "diff",
		},
		"simplify": {
			Name:        "simplify",
			Description: "简化当前 diff",
			Prompt:      "请基于下面这份 diff 提出简化方案，优先减少复杂度、重复和不必要抽象。",
			ResultView:  "review-card",
			TargetType:  "diff",
		},
		"debug": {
			Name:        "debug",
			Description: "分析当前错误",
			Prompt:      "请分析当前错误上下文，给出最可能原因、定位思路和下一步修复建议。",
			ResultView:  "review-card",
			TargetType:  "error",
		},
		"security-review": {
			Name:        "security-review",
			Description: "安全审查当前 diff",
			Prompt:      "请对下面这份 diff 做安全审查，重点关注认证、授权、注入、敏感数据暴露和危险默认值。",
			ResultView:  "review-card",
			TargetType:  "diff",
		},
		"explain-step": {
			Name:        "explain-step",
			Description: "解释当前步骤",
			Prompt:      "请解释下面这个步骤正在做什么、它为什么重要，以及我应该关注哪些输出或副作用。",
			ResultView:  "review-card",
			TargetType:  "step",
		},
		"next-step": {
			Name:        "next-step",
			Description: "推断下一步",
			Prompt:      "请基于下面这个当前步骤上下文，推断最可能的下一步动作、需要检查的点，以及如果失败应如何继续定位。",
			ResultView:  "review-card",
			TargetType:  "step",
		},
	}
}

func MetaForSkill(def Definition, target, targetPath, contextID, contextTitle, targetText string) protocol.RuntimeMeta {
	return protocol.RuntimeMeta{
		Source:       "skill-center",
		SkillName:    def.Name,
		Target:       target,
		TargetType:   def.TargetType,
		TargetPath:   targetPath,
		ResultView:   def.ResultView,
		ContextID:    contextID,
		ContextTitle: contextTitle,
		TargetText:   targetText,
	}
}
