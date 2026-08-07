package provisioning

import (
	"strings"
	"time"

	scimfilter "github.com/scim2/filter-parser/v2"
	"github.com/uptrace/bun"
)

const maxFilterBytes = 4096

type filterTarget struct {
	expression string
	caseExact  bool
	boolean    bool
	timestamp  bool
}

func applySCIMFilter(query *bun.SelectQuery, raw, resourceType, dialect string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) > maxFilterBytes {
		return ErrInvalidFilter
	}
	expression, err := scimfilter.ParseFilter([]byte(raw))
	if err != nil {
		return ErrInvalidFilter
	}
	nodes := 0
	if err := addFilterExpression(query, expression, resourceType, dialect, " AND ", false, 0, &nodes); err != nil {
		return ErrInvalidFilter
	}
	return nil
}

func addFilterExpression(query *bun.SelectQuery, expression scimfilter.Expression, resourceType, dialect, separator string, negate bool, depth int, nodes *int) error {
	(*nodes)++
	if depth > 16 || *nodes > 64 {
		return ErrInvalidFilter
	}
	switch value := expression.(type) {
	case *scimfilter.AttributeExpression:
		condition, args, err := compileAttribute(*value, resourceType, dialect, "")
		if err != nil {
			return err
		}
		if negate {
			condition = "NOT (" + condition + ")"
		}
		addWhere(query, separator, condition, args...)
		return nil
	case *scimfilter.LogicalExpression:
		if value.Operator != scimfilter.AND && value.Operator != scimfilter.OR {
			return ErrInvalidFilter
		}
		operator := value.Operator
		if negate {
			if operator == scimfilter.AND {
				operator = scimfilter.OR
			} else {
				operator = scimfilter.AND
			}
		}
		var groupErr error
		query.WhereGroup(separator, func(group *bun.SelectQuery) *bun.SelectQuery {
			groupErr = addFilterExpression(group, value.Left, resourceType, dialect, " AND ", negate, depth+1, nodes)
			if groupErr != nil {
				return group
			}
			rightSeparator := " AND "
			if operator == scimfilter.OR {
				rightSeparator = " OR "
			}
			groupErr = addFilterExpression(group, value.Right, resourceType, dialect, rightSeparator, negate, depth+1, nodes)
			return group
		})
		return groupErr
	case *scimfilter.NotExpression:
		return addFilterExpression(query, value.Expression, resourceType, dialect, separator, !negate, depth+1, nodes)
	case *scimfilter.ValuePath:
		attribute := strings.ToLower(value.AttributePath.AttributeName)
		if attribute != "emails" && attribute != "members" {
			return ErrInvalidFilter
		}
		inner, ok := value.ValueFilter.(*scimfilter.AttributeExpression)
		if !ok {
			return ErrInvalidFilter
		}
		condition, args, err := compileAttribute(*inner, resourceType, dialect, attribute)
		if err != nil {
			return err
		}
		if negate {
			condition = "NOT (" + condition + ")"
		}
		addWhere(query, separator, condition, args...)
		return nil
	default:
		return ErrInvalidFilter
	}
}

func compileAttribute(expression scimfilter.AttributeExpression, resourceType, dialect, parent string) (string, []any, error) {
	path := expression.AttributePath
	if path.URIPrefix != nil {
		allowed := UserSchema
		if resourceType == ResourceGroup {
			allowed = GroupSchema
		}
		if !strings.EqualFold(*path.URIPrefix, allowed) {
			return "", nil, ErrInvalidFilter
		}
	}
	name := strings.ToLower(path.AttributeName)
	if path.SubAttribute != nil {
		name += "." + strings.ToLower(*path.SubAttribute)
	}
	if parent != "" {
		name = strings.ToLower(parent) + "." + name
	}
	if resourceType == ResourceGroup && name == "members.value" {
		return compileMemberFilter(expression.Operator, expression.CompareValue)
	}
	target, ok := filterAttributes(resourceType)[name]
	if !ok {
		return "", nil, ErrInvalidFilter
	}
	return compileComparison(target, expression.Operator, expression.CompareValue, dialect)
}

func filterAttributes(resourceType string) map[string]filterTarget {
	common := map[string]filterTarget{
		"id":                {expression: "resource.resource_id", caseExact: true},
		"externalid":        {expression: "resource.external_id", caseExact: true},
		"meta.created":      {expression: "resource.created_at", timestamp: true},
		"meta.lastmodified": {expression: "resource.updated_at", timestamp: true},
	}
	if resourceType == ResourceUser {
		common["username"] = filterTarget{expression: "principal.login_name"}
		common["displayname"] = filterTarget{expression: "principal.display_name"}
		common["name.formatted"] = filterTarget{expression: "principal.display_name"}
		common["emails.value"] = filterTarget{expression: "principal.email"}
		common["active"] = filterTarget{expression: "(principal.status = 'active' AND member.status = 'active')", boolean: true}
	} else if resourceType == ResourceGroup {
		common["displayname"] = filterTarget{expression: "scim_group.name"}
	}
	return common
}

func compileComparison(target filterTarget, operator scimfilter.CompareOperator, value any, dialect string) (string, []any, error) {
	if operator == scimfilter.PR {
		if target.boolean {
			return "1 = 1", nil, nil
		}
		return target.expression + " <> ''", nil, nil
	}
	if target.boolean {
		boolean, ok := value.(bool)
		if !ok || operator != scimfilter.EQ && operator != scimfilter.NE {
			return "", nil, ErrInvalidFilter
		}
		positive := boolean == (operator == scimfilter.EQ)
		if positive {
			return target.expression, nil, nil
		}
		return "NOT " + target.expression, nil, nil
	}
	if value == nil {
		if operator == scimfilter.EQ {
			return target.expression + " IS NULL", nil, nil
		}
		if operator == scimfilter.NE {
			return target.expression + " IS NOT NULL", nil, nil
		}
		return "", nil, ErrInvalidFilter
	}
	text, ok := value.(string)
	if !ok {
		return "", nil, ErrInvalidFilter
	}
	if target.timestamp {
		parsed, err := time.Parse(time.RFC3339, text)
		if err != nil || operator == scimfilter.CO || operator == scimfilter.SW || operator == scimfilter.EW {
			return "", nil, ErrInvalidFilter
		}
		return comparison(target.expression, operator, parsed.UTC().UnixMilli(), dialect)
	}
	column, argument := target.expression, text
	if !target.caseExact {
		column, argument = "LOWER("+column+")", strings.ToLower(text)
	}
	return comparison(column, operator, argument, dialect)
}

func comparison(column string, operator scimfilter.CompareOperator, argument any, dialect string) (string, []any, error) {
	switch operator {
	case scimfilter.EQ:
		return column + " = ?", []any{argument}, nil
	case scimfilter.NE:
		return column + " <> ?", []any{argument}, nil
	case scimfilter.GT:
		return column + " > ?", []any{argument}, nil
	case scimfilter.GE:
		return column + " >= ?", []any{argument}, nil
	case scimfilter.LT:
		return column + " < ?", []any{argument}, nil
	case scimfilter.LE:
		return column + " <= ?", []any{argument}, nil
	case scimfilter.CO:
		if dialect == "mysql" {
			return "LOCATE(?, " + column + ") > 0", []any{argument}, nil
		}
		if dialect == "postgres" {
			return "STRPOS(" + column + ", ?) > 0", []any{argument}, nil
		}
		return "INSTR(" + column + ", ?) > 0", []any{argument}, nil
	case scimfilter.SW:
		return "SUBSTR(" + column + ", 1, LENGTH(?)) = ?", []any{argument, argument}, nil
	case scimfilter.EW:
		return "SUBSTR(" + column + ", -LENGTH(?)) = ?", []any{argument, argument}, nil
	default:
		return "", nil, ErrInvalidFilter
	}
}

func compileMemberFilter(operator scimfilter.CompareOperator, value any) (string, []any, error) {
	const exists = "EXISTS (SELECT 1 FROM iam_group_members AS filter_member WHERE filter_member.tenant_id = directory.tenant_id AND filter_member.group_id = resource.resource_id"
	if operator == scimfilter.PR {
		return exists + ")", nil, nil
	}
	memberID, ok := value.(string)
	if !ok || memberID == "" || operator != scimfilter.EQ && operator != scimfilter.NE {
		return "", nil, ErrInvalidFilter
	}
	condition := exists + " AND filter_member.principal_id = ?)"
	if operator == scimfilter.NE {
		condition = "NOT " + condition
	}
	return condition, []any{memberID}, nil
}

func addWhere(query *bun.SelectQuery, separator, condition string, args ...any) {
	if strings.Contains(strings.ToUpper(separator), "OR") {
		query.WhereOr(condition, args...)
		return
	}
	query.Where(condition, args...)
}

func normalizeDialect(db *bun.DB) string {
	name := db.Dialect().Name().String()
	if name == "pg" {
		return "postgres"
	}
	return name
}
