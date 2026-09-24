package middleware

import (
	"strings"

	"go-admin/internal/logger"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"gorm.io/gorm"
)

// CasbinRule 对应 casbin_rule 表，用于持久化权限策略。
// 各列宽度需 ≥ sys_menu.permission 的 200 字符，否则长权限码写入会被截断或报错。
type CasbinRule struct {
	ID    uint   `gorm:"primarykey"`
	Ptype string `gorm:"column:ptype;size:200"`
	V0    string `gorm:"column:v0;size:200"`
	V1    string `gorm:"column:v1;size:200"`
	V2    string `gorm:"column:v2;size:200"`
	V3    string `gorm:"column:v3;size:200"`
	V4    string `gorm:"column:v4;size:200"`
	V5    string `gorm:"column:v5;size:200"`
}

func (CasbinRule) TableName() string {
	return "casbin_rule"
}

// gormAdapter 基于 GORM 读写 Casbin 策略，替代 casbin 官方的 gorm-adapter 依赖
type gormAdapter struct {
	db *gorm.DB
}

func newGormAdapter(db *gorm.DB) *gormAdapter {
	return &gormAdapter{db: db}
}

var casbinRuleColumns = []string{"v0", "v1", "v2", "v3", "v4", "v5"}

// ruleToLine 把一行策略拼成 Casbin 文本格式（如 "p, admin, default, *, *"），
// 并去掉尾部空字段，避免解析出多余的空列
func ruleToLine(r CasbinRule) string {
	vals := []string{r.Ptype, r.V0, r.V1, r.V2, r.V3, r.V4, r.V5}
	end := len(vals)
	for end > 1 && vals[end-1] == "" {
		end--
	}
	return strings.Join(vals[:end], ", ")
}

// buildRule 把 Casbin 的规则切片映射为数据库行
func buildRule(ptype string, rule []string) CasbinRule {
	r := CasbinRule{Ptype: ptype}
	fields := []*string{&r.V0, &r.V1, &r.V2, &r.V3, &r.V4, &r.V5}
	for i, v := range rule {
		if i < len(fields) {
			*fields[i] = v
		}
	}
	return r
}

func (a *gormAdapter) LoadPolicy(m model.Model) error {
	var rules []CasbinRule
	if err := a.db.Find(&rules).Error; err != nil {
		return err
	}
	for _, r := range rules {
		line := ruleToLine(r)
		if err := persist.LoadPolicyLine(line, m); err != nil {
			// 单行策略格式非法（列数不对、ptype 不认识等）：记录并跳过。
			// 不让整个加载失败，是因为失败会让服务起不来；
			// 跳过的后果是「这条策略不生效」，方向是 fail-closed（少一条授权），
			// 而且日志里留下了原始行，便于定位是库里哪条数据坏了。
			logger.Log.Errorf("[casbin] 策略行解析失败，已跳过: line=%q err=%v", line, err)
		}
	}
	return nil
}

func (a *gormAdapter) SavePolicy(m model.Model) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		// 全量重建：先清空再写入
		if err := tx.Where("1 = 1").Delete(&CasbinRule{}).Error; err != nil {
			return err
		}

		var rules []CasbinRule
		for _, sec := range []string{"p", "g"} {
			for ptype, ast := range m[sec] {
				for _, rule := range ast.Policy {
					rules = append(rules, buildRule(ptype, rule))
				}
			}
		}
		if len(rules) == 0 {
			return nil
		}
		return tx.Create(&rules).Error
	})
}

func (a *gormAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	r := buildRule(ptype, rule)
	return a.db.Create(&r).Error
}

// AddPolicies 批量写入策略。
// 必须实现：casbin 的 Enforcer.AddPolicies 会对适配器做不带检查的
// persist.BatchAdapter 类型断言，缺失该方法会直接 panic。
func (a *gormAdapter) AddPolicies(sec string, ptype string, rules [][]string) error {
	if len(rules) == 0 {
		return nil
	}
	rows := make([]CasbinRule, 0, len(rules))
	for _, rule := range rules {
		rows = append(rows, buildRule(ptype, rule))
	}
	return a.db.Create(&rows).Error
}

// RemovePolicies 批量删除策略，同样属于 persist.BatchAdapter 接口
func (a *gormAdapter) RemovePolicies(sec string, ptype string, rules [][]string) error {
	return a.db.Transaction(func(tx *gorm.DB) error {
		for _, rule := range rules {
			q := tx.Where("ptype = ?", ptype)
			for i, v := range rule {
				if i < len(casbinRuleColumns) {
					q = q.Where(casbinRuleColumns[i]+" = ?", v)
				}
			}
			if err := q.Delete(&CasbinRule{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *gormAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	q := a.db.Where("ptype = ?", ptype)
	for i, v := range rule {
		if i < len(casbinRuleColumns) {
			q = q.Where(casbinRuleColumns[i]+" = ?", v)
		}
	}
	return q.Delete(&CasbinRule{}).Error
}

func (a *gormAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	q := a.db.Where("ptype = ?", ptype)
	for i, v := range fieldValues {
		idx := fieldIndex + i
		if v != "" && idx >= 0 && idx < len(casbinRuleColumns) {
			q = q.Where(casbinRuleColumns[idx]+" = ?", v)
		}
	}
	return q.Delete(&CasbinRule{}).Error
}

var (
	_ persist.Adapter      = (*gormAdapter)(nil)
	_ persist.BatchAdapter = (*gormAdapter)(nil)
)
