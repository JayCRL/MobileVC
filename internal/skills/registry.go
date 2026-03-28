package skills

import (
	"context"
	"sort"
	"strings"
	"time"

	"mobilevc/internal/protocol"
	"mobilevc/internal/store"
)

type Definition struct {
	Name        string
	Description string
	Prompt      string
	ResultView  string
	TargetType  string
	Source      store.SkillSource
	Editable    bool
	UpdatedAt   time.Time
}

type Registry struct {
	store store.Store
}

func Builtins() map[string]Definition {
	return map[string]Definition{
		"review": {
			Name:        "review",
			Description: "审查当前 diff",
			Prompt:      "请审查下面这份 diff，重点指出 bug、可维护性问题、回归风险和可执行改进建议。",
			ResultView:  "review-card",
			TargetType:  "diff",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"analyze": {
			Name:        "analyze",
			Description: "分析当前上下文",
			Prompt:      "请分析下面的上下文，优先给出问题归因、当前状态判断、最值得执行的下一步，以及必要时的验证建议。",
			ResultView:  "review-card",
			TargetType:  "diff",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"flutter-context": {
			Name:        "flutter-context",
			Description: "理解 Flutter 端上下文时优先复用已启用记忆索引",
			Prompt:      "请先阅读上方已注入的 MobileVC memory，优先复用其中与 Flutter 端相关的架构索引、函数逻辑索引和输入/权限生命周期记录来回答问题或理解上下文。默认不要重新全量扫描 Flutter 代码；如果这些记忆不足以支撑当前任务，再明确指出还需要补充读取的具体 Flutter 文件、函数或模块，并说明为什么。",
			ResultView:  "review-card",
			TargetType:  "context",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"doctor": {
			Name:        "doctor",
			Description: "诊断当前上下文",
			Prompt:      "请基于下面的上下文做诊断，输出最可能的问题类别、建议检查项、推荐执行顺序，以及需要人工确认的风险点。",
			ResultView:  "review-card",
			TargetType:  "error",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"simplify": {
			Name:        "simplify",
			Description: "简化当前 diff",
			Prompt:      "请基于下面这份 diff 提出简化方案，优先减少复杂度、重复和不必要抽象。",
			ResultView:  "review-card",
			TargetType:  "diff",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"debug": {
			Name:        "debug",
			Description: "分析当前错误",
			Prompt:      "请分析当前错误上下文，给出最可能原因、定位思路和下一步修复建议。",
			ResultView:  "review-card",
			TargetType:  "error",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"security-review": {
			Name:        "security-review",
			Description: "安全审查当前 diff",
			Prompt:      "请对下面这份 diff 做安全审查，重点关注认证、授权、注入、敏感数据暴露和危险默认值。",
			ResultView:  "review-card",
			TargetType:  "diff",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"explain-step": {
			Name:        "explain-step",
			Description: "解释当前步骤",
			Prompt:      "请解释下面这个步骤正在做什么、它为什么重要，以及我应该关注哪些输出或副作用。",
			ResultView:  "review-card",
			TargetType:  "step",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
		"next-step": {
			Name:        "next-step",
			Description: "推断下一步",
			Prompt:      "请基于下面这个当前步骤上下文，推断最可能的下一步动作、需要检查的点，以及如果失败应如何继续定位。",
			ResultView:  "review-card",
			TargetType:  "step",
			Source:      store.SkillSourceBuiltin,
			Editable:    false,
		},
	}
}

func NewRegistry(skillStore store.Store) *Registry {
	return &Registry{store: skillStore}
}

func (r *Registry) ListSkills() ([]Definition, error) {
	merged := make(map[string]Definition)
	for name, def := range Builtins() {
		merged[name] = def
	}
	if r == nil || r.store == nil {
		return sortDefinitions(merged), nil
	}
	persisted, err := r.store.ListSkillCatalog(contextBackground())
	if err != nil {
		return nil, err
	}
	for _, item := range persisted {
		def := fromStoreSkill(item)
		existing, ok := merged[def.Name]
		if !ok {
			merged[def.Name] = def
			continue
		}
		switch def.Source {
		case store.SkillSourceLocal:
			merged[def.Name] = mergeDefinition(existing, def)
		case store.SkillSourceExternal:
			merged[def.Name] = mergeExternalDefinition(existing, def)
		}
	}
	return sortDefinitions(merged), nil
}

func (r *Registry) GetSkill(name string) (Definition, bool, error) {
	items, err := r.ListSkills()
	if err != nil {
		return Definition{}, false, err
	}
	needle := strings.TrimSpace(name)
	for _, item := range items {
		if item.Name == needle {
			return item, true, nil
		}
	}
	return Definition{}, false, nil
}

func (r *Registry) UpsertLocalSkill(def Definition) error {
	if r == nil || r.store == nil {
		return nil
	}
	items, err := r.store.ListSkillCatalog(contextBackground())
	if err != nil {
		return err
	}
	def.Source = store.SkillSourceLocal
	def.Editable = true
	if def.UpdatedAt.IsZero() {
		def.UpdatedAt = time.Now().UTC()
	}
	storeDef := toStoreSkill(def)
	updated := false
	for i := range items {
		if items[i].Name == storeDef.Name {
			items[i] = storeDef
			updated = true
			break
		}
	}
	if !updated {
		items = append(items, storeDef)
	}
	return r.store.SaveSkillCatalog(contextBackground(), items)
}

func (r *Registry) SyncExternalSkills(items []Definition) error {
	if r == nil || r.store == nil {
		return nil
	}
	persisted, err := r.store.ListSkillCatalog(contextBackground())
	if err != nil {
		return err
	}
	filtered := make([]store.SkillDefinition, 0, len(persisted)+len(items))
	for _, item := range persisted {
		if item.Source != store.SkillSourceExternal {
			filtered = append(filtered, item)
		}
	}
	for _, item := range items {
		item.Source = store.SkillSourceExternal
		item.Editable = true
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = time.Now().UTC()
		}
		filtered = append(filtered, toStoreSkill(item))
	}
	return r.store.SaveSkillCatalog(contextBackground(), filtered)
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

func mergeDefinition(base, overlay Definition) Definition {
	if strings.TrimSpace(overlay.Description) != "" {
		base.Description = overlay.Description
	}
	if strings.TrimSpace(overlay.Prompt) != "" {
		base.Prompt = overlay.Prompt
	}
	if strings.TrimSpace(overlay.ResultView) != "" {
		base.ResultView = overlay.ResultView
	}
	if strings.TrimSpace(overlay.TargetType) != "" {
		base.TargetType = overlay.TargetType
	}
	base.Source = overlay.Source
	base.Editable = overlay.Editable
	if !overlay.UpdatedAt.IsZero() {
		base.UpdatedAt = overlay.UpdatedAt
	}
	return base
}

func mergeExternalDefinition(base, overlay Definition) Definition {
	if base.Source == store.SkillSourceBuiltin {
		if strings.TrimSpace(base.Description) == "" && strings.TrimSpace(overlay.Description) != "" {
			base.Description = overlay.Description
		}
		if strings.TrimSpace(base.ResultView) == "" && strings.TrimSpace(overlay.ResultView) != "" {
			base.ResultView = overlay.ResultView
		}
		if strings.TrimSpace(base.TargetType) == "" && strings.TrimSpace(overlay.TargetType) != "" {
			base.TargetType = overlay.TargetType
		}
		return base
	}
	return mergeDefinition(base, overlay)
}

func sortDefinitions(items map[string]Definition) []Definition {
	result := make([]Definition, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func fromStoreSkill(item store.SkillDefinition) Definition {
	return Definition{
		Name:        item.Name,
		Description: item.Description,
		Prompt:      item.Prompt,
		ResultView:  item.ResultView,
		TargetType:  item.TargetType,
		Source:      item.Source,
		Editable:    item.Editable,
		UpdatedAt:   item.UpdatedAt,
	}
}

func toStoreSkill(item Definition) store.SkillDefinition {
	return store.SkillDefinition{
		Name:        strings.TrimSpace(item.Name),
		Description: strings.TrimSpace(item.Description),
		Prompt:      strings.TrimSpace(item.Prompt),
		ResultView:  strings.TrimSpace(item.ResultView),
		TargetType:  strings.TrimSpace(item.TargetType),
		Source:      item.Source,
		Editable:    item.Editable,
		UpdatedAt:   item.UpdatedAt,
	}
}

func contextBackground() context.Context {
	return context.Background()
}
