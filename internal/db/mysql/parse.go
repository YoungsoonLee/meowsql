package mysql

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/format"
	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
)

// ValidateSQL parses the input as MySQL 8 (pingcap/tidb parser — CTEs, window
// functions, JSON) and returns a fingerprint over the canonical restored form.
func ValidateSQL(sql string) (string, error) {
	stmt, err := parseOne(sql)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	rc := format.NewRestoreCtx(format.DefaultRestoreFlags, &buf)
	if err := stmt.Restore(rc); err != nil {
		return "", fmt.Errorf("restore: %w", err)
	}
	sum := sha1.Sum(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// ValidateOnly matches the agent.Validator signature for CLI wiring.
func ValidateOnly(sql string) error {
	_, err := ValidateSQL(sql)
	return err
}

// parseTableNames walks the AST collecting every referenced table. Used as a
// fallback when an EXPLAIN plan is unavailable (schema-only mode).
func parseTableNames(sql string) ([]string, error) {
	stmt, err := parseOne(sql)
	if err != nil {
		return nil, err
	}
	v := &tableVisitor{seen: map[string]struct{}{}}
	stmt.Accept(v)
	return v.tables, nil
}

func parseOne(sql string) (ast.StmtNode, error) {
	stmts, _, err := parser.New().Parse(sql, "", "")
	if err != nil {
		return nil, err
	}
	if len(stmts) == 0 {
		return nil, fmt.Errorf("empty statement")
	}
	return stmts[0], nil
}

type tableVisitor struct {
	tables []string
	seen   map[string]struct{}
}

func (v *tableVisitor) Enter(n ast.Node) (ast.Node, bool) {
	if t, ok := n.(*ast.TableName); ok {
		full := t.Name.O
		if t.Schema.O != "" {
			full = t.Schema.O + "." + full
		}
		if _, dup := v.seen[full]; !dup && full != "" {
			v.seen[full] = struct{}{}
			v.tables = append(v.tables, full)
		}
	}
	return n, false
}

func (v *tableVisitor) Leave(n ast.Node) (ast.Node, bool) { return n, true }
