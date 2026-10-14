// 文件用途：校验探索标注的适用范围；范围独立于长期、临时和敏感类别。
package annotator

import (
	"fmt"
	"strings"

	"memory-spike/internal/spike"
)

// validateScope 保留旧数据的空范围（未知），项目草稿可暂缺名称，确认或完成时必须补齐。
func validateScope(g spike.Gold, confirmed bool) error {
	switch g.Scope {
	case "", "global", "project", "conversation":
	default:
		return fmt.Errorf("适用范围无效")
	}
	if g.Scope != "project" && g.Project != "" {
		return fmt.Errorf("只有项目范围可以填写项目名称")
	}
	if g.Scope == "project" && confirmed && strings.TrimSpace(g.Project) == "" {
		return fmt.Errorf("仅某个项目适用的事实需要填写项目名称")
	}
	return nil
}
