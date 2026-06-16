package workflow

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Scope is the variable bindings visible to expressions. Workflows
// nest scopes (foreach iterations push a child) so a name lookup walks
// the chain. Bindings are immutable post-write — once a step assigns
// `$obs`, no later step can reassign the same name in the same scope.
// This keeps expressions referentially transparent and removes whole
// classes of "what value did this name have when?" debug surprises.
type Scope struct {
	parent *Scope
	vars   map[string]any
}

// NewScope creates an empty top-level scope.
func NewScope() *Scope {
	return &Scope{vars: map[string]any{}}
}

// Child returns a fresh scope chained to s. Look-ups not found locally
// fall through to the parent.
func (s *Scope) Child() *Scope {
	return &Scope{parent: s, vars: map[string]any{}}
}

// Set binds name → value in the local frame. Returns an error if name
// is already bound in this exact frame; callers SHOULD treat that as a
// programming error in the workflow definition (two save_as on the
// same name in the same scope).
func (s *Scope) Set(name string, value any) error {
	if _, exists := s.vars[name]; exists {
		return fmt.Errorf("variable %q already bound in this scope", name)
	}
	s.vars[name] = value
	return nil
}

// Get walks the scope chain and returns (value, true) on hit.
func (s *Scope) Get(name string) (any, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if v, ok := cur.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// Resolve looks up a variable by name OR a dotted path. The leading "$"
// is optional, so "$obs.water.count" and "obs.water.count" both work.
// Returns (value, true) on success; (nil, false) when any segment is
// missing.
func (s *Scope) Resolve(path string) (any, bool) {
	path = strings.TrimPrefix(path, "$")
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	cur, ok := s.Get(parts[0])
	if !ok {
		return nil, false
	}
	for _, p := range parts[1:] {
		cur, ok = walkOne(cur, p)
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// walkOne descends one path segment. Supports map[string]any (the
// canonical JSON shape we get from MCP tool responses), nested
// Definitions/structs are not navigable — workflows always go through
// the JSON-marshalled form first.
func walkOne(cur any, segment string) (any, bool) {
	switch m := cur.(type) {
	case map[string]any:
		v, ok := m[segment]
		return v, ok
	default:
		return nil, false
	}
}

// ── expressions ────────────────────────────────────────────────────────
//
// We keep the expression dialect intentionally tiny: variable lookup,
// literal numbers / strings / booleans, the comparison operators
// == != < <= > >=, the logical operators && || !, and parens. No
// arithmetic, no function calls, no string interpolation. Any step that
// needs more should compute it Go-side and bind via save_as.
//
// Grammar (informal):
//   expr   := orExpr
//   orExpr := andExpr ( '||' andExpr )*
//   andExpr:= notExpr ( '&&' notExpr )*
//   notExpr:= '!' notExpr | atom
//   atom   := '(' expr ')' | comparison | path | literal
//   comparison := path op operand        (path on left only — keeps it cheap)
//   op     := '==' | '!=' | '<' | '<=' | '>' | '>='
//   operand:= literal | path
//   literal:= number | quoted-string | 'true' | 'false' | 'nil'
//
// EvalBool returns the truthy interpretation of an expression. nil and
// missing variables are falsy; numbers are truthy when non-zero;
// strings when non-empty; lists/maps when non-empty.

// EvalBool evaluates an expression string against the scope and returns
// its truthiness.
func EvalBool(expr string, scope *Scope) (bool, error) {
	v, err := Eval(expr, scope)
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

// Eval evaluates an expression to a Go value. Errors only surface for
// genuine syntax problems; missing variables resolve to nil so steps
// can safely test optional inputs.
func Eval(expr string, scope *Scope) (any, error) {
	p := &parser{src: strings.TrimSpace(expr), scope: scope}
	val, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	p.skipWs()
	if p.pos < len(p.src) {
		return nil, fmt.Errorf("unexpected trailing input at position %d in %q", p.pos, expr)
	}
	return val, nil
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		// Unknown types default to truthy (presence implies meaning).
		return true
	}
}

// ── parser ──────────────────────────────────────────────────────────────

type parser struct {
	src   string
	pos   int
	scope *Scope
}

func (p *parser) skipWs() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) peek(s string) bool {
	p.skipWs()
	return strings.HasPrefix(p.src[p.pos:], s)
}

func (p *parser) consume(s string) bool {
	if p.peek(s) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *parser) parseOr() (any, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.consume("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		// Short-circuit on the value level: || returns the first truthy.
		if truthy(left) {
			continue
		}
		left = right
	}
	return left, nil
}

func (p *parser) parseAnd() (any, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.consume("&&") {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		if !truthy(left) {
			continue
		}
		left = right
	}
	return left, nil
}

func (p *parser) parseNot() (any, error) {
	if p.consume("!") {
		v, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return !truthy(v), nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (any, error) {
	p.skipWs()
	if p.consume("(") {
		v, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.consume(")") {
			return nil, errors.New("missing closing parenthesis")
		}
		return v, nil
	}
	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	p.skipWs()
	if op, ok := p.tryComparison(); ok {
		right, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return compare(left, op, right)
	}
	return left, nil
}

// Comparison operator detection. Order matters: longer first so we
// don't mis-parse `==` as `=` or `<=` as `<`.
var compOps = []string{"==", "!=", "<=", ">=", "<", ">"}

func (p *parser) tryComparison() (string, bool) {
	for _, op := range compOps {
		if p.consume(op) {
			return op, true
		}
	}
	return "", false
}

// parseOperand reads either a literal or a path. The first character
// disambiguates; numbers / quotes / true|false|nil go literal, anything
// else (letter or $) is a path.
func (p *parser) parseOperand() (any, error) {
	p.skipWs()
	if p.pos >= len(p.src) {
		return nil, errors.New("unexpected end of expression")
	}
	c := p.src[p.pos]
	switch {
	case c == '"' || c == '\'':
		return p.parseString(c)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	case c == '$' || isIdent(c):
		return p.parsePathOrLiteral()
	default:
		return nil, fmt.Errorf("unexpected character %q at position %d", c, p.pos)
	}
}

func (p *parser) parseString(quote byte) (any, error) {
	p.pos++ // consume open quote
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != quote {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return nil, errors.New("unterminated string literal")
	}
	s := p.src[start:p.pos]
	p.pos++ // consume close quote
	return s, nil
}

func (p *parser) parseNumber() (any, error) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if (c >= '0' && c <= '9') || c == '.' {
			p.pos++
		} else {
			break
		}
	}
	tok := p.src[start:p.pos]
	if strings.Contains(tok, ".") {
		f, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	i, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return nil, err
	}
	// Up-cast to float64 so all numerics share one branch in compare().
	return float64(i), nil
}

func (p *parser) parsePathOrLiteral() (any, error) {
	start := p.pos
	if p.src[p.pos] == '$' {
		p.pos++
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if isIdent(c) || c == '.' {
			p.pos++
		} else {
			break
		}
	}
	tok := p.src[start:p.pos]
	switch tok {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "nil", "null":
		return nil, nil
	}
	v, _ := p.scope.Resolve(tok)
	return v, nil
}

func isIdent(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}

// ── comparisons ─────────────────────────────────────────────────────────

func compare(left any, op string, right any) (any, error) {
	// Equality first — applies to any pair.
	if op == "==" || op == "!=" {
		eq := equalAny(left, right)
		if op == "!=" {
			eq = !eq
		}
		return eq, nil
	}
	// Ordering only on numerics or strings.
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if lok && rok {
		switch op {
		case "<":
			return lf < rf, nil
		case "<=":
			return lf <= rf, nil
		case ">":
			return lf > rf, nil
		case ">=":
			return lf >= rf, nil
		}
	}
	ls, lsok := left.(string)
	rs, rsok := right.(string)
	if lsok && rsok {
		switch op {
		case "<":
			return ls < rs, nil
		case "<=":
			return ls <= rs, nil
		case ">":
			return ls > rs, nil
		case ">=":
			return ls >= rs, nil
		}
	}
	// Mixed / unsupported: comparison reads as false rather than blowing
	// up the whole workflow — defensive default for production traces.
	return false, nil
}

func equalAny(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
