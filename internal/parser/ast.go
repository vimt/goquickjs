package parser

type Node interface{ astNode() }

type Program struct{ Body []Node }

// Statements.

type ExprStmt struct{ X Node }
type LetDecl struct {
	Name string
	Init Node // may be nil
}

// MultiVarDecl is what we synthesise for `let a = 1, b = 2;` — a flat
// list of single-binding LetDecls. Compiler emits them sequentially.
type MultiVarDecl struct {
	Decls []Node // each is *LetDecl or *DestructureDecl
}

// DestructureDecl handles `let {a, b} = obj` and `let [x, y] = arr`.
// The compiler stashes Init into a fresh temp, then for each binding
// emits a regular declare + pull-and-store.
type DestructureDecl struct {
	Pattern Pattern
	Init    Node
}

// Pattern is either ObjectPattern or ArrayPattern. They share the
// same emit logic: walk children, pull a value via Key/Index, recurse
// for nested patterns, fall back to Default when source value is
// undefined.
type Pattern interface {
	patternMarker()
	astNode()
}

// ObjectPatternProp represents one slot in `{a, b: x, c = 1, ...rest}`.
//   - Plain shorthand `a`: Key="a", Target=&IdentTarget{Name:"a"}.
//   - Renamed `b: x`: Key="b", Target=&IdentTarget{Name:"x"}.
//   - Default `c = 1`: Default set; Target's IdentTarget.Name binds.
//   - Rest `...rest`: IsRest=true; Target.Name binds the leftovers obj.
type ObjectPatternProp struct {
	Key     string
	Target  PatternTarget
	Default Node
	IsRest  bool
}

// ArrayPatternElem covers `[a, , b = 1, ...rest]` slots:
//   - Empty slot (hole) — Skip=true; the position is just consumed.
//   - Normal/default — Target+Default.
//   - Rest — IsRest=true; binds leftover Array.
type ArrayPatternElem struct {
	Target  PatternTarget
	Default Node
	IsRest  bool
	Skip    bool
}

// PatternTarget is either a plain binding name or a nested Pattern.
type PatternTarget interface{ patternTargetMarker() }

// IdentTarget binds the name as a fresh local.
type IdentTarget struct{ Name string }

// NestedTarget recursively destructures into the inner pattern.
type NestedTarget struct{ Pattern Pattern }

func (*IdentTarget) patternTargetMarker()  {}
func (*NestedTarget) patternTargetMarker() {}

type ObjectPattern struct{ Props []ObjectPatternProp }
type ArrayPattern struct{ Elements []ArrayPatternElem }

func (*ObjectPattern) patternMarker() {}
func (*ArrayPattern) patternMarker()  {}
type Block struct{ Body []Node }
type ForStmt struct {
	Init   Node // *LetDecl | *ExprStmt | nil
	Test   Node // expression | nil
	Update Node // expression | nil
	Body   Node // statement
}

// ForOfStmt is `for (let|var name of iterable) body`. Iterates via
// the JS iterator protocol — pulls a `next()`-bearing iterator from
// the iterable, loops binding `.value` to `name` until `.done`.
// AssignTo is set instead of Name for the assignment-style form
// `for (existingVar of arr)` / `for ({a, b} of arr)`: the loop value
// is stored into the existing LHS via emitDestructureAssign-style
// resolve+store. Pattern destructuring in either flavour goes through
// AssignTo (set to the Pattern) — declaration destructuring isn't yet
// supported but the corpus form is overwhelmingly assignment-style.
type ForOfStmt struct {
	Name     string // declaration form (let/var)
	AssignTo Node   // assignment form: Ident, or Pattern (ObjectPattern/ArrayPattern)
	Iterable Node
	Body     Node
}

// ForInStmt is `for (let|var name in obj) body`. Yields each
// enumerable own string key of obj (Array indices included as
// strings, per spec). Inherited props deliberately excluded —
// we'd need to walk the proto chain otherwise.
type ForInStmt struct {
	Name     string
	AssignTo Node
	Obj      Node
	Body     Node
}

type IfStmt struct {
	Test Node
	Cons Node
	Alt  Node // may be nil
}

type ReturnStmt struct{ Arg Node } // Arg may be nil

type ThrowStmt struct{ Arg Node }

// TryStmt covers try { ... } [catch (e) { ... }] [finally { ... }].
// At least one of CatchBody / FinallyBody must be present.
type TryStmt struct {
	Body         *Block
	CatchParam   string
	CatchBody    *Block // nil when there's no catch clause
	FinallyBody  *Block // nil when there's no finally clause
}

type WhileStmt struct {
	Test Node
	Body Node
}

type BreakStmt struct{ Label string }
type ContinueStmt struct{ Label string }

// LabeledStmt is `name: stmt`. The compiler attaches Label to the
// innermost loop / switch frame so a labeled break/continue inside
// the body can target it. Only matters when Body is a loop or switch.
type LabeledStmt struct {
	Label string
	Body  Node
}

type SwitchStmt struct {
	Discriminant Node
	Cases        []SwitchCase
}

// SwitchCase: Test==nil means `default:`.
type SwitchCase struct {
	Test Node
	Body []Node
}

// Param is one formal parameter of a function. Pattern!=nil means a
// destructuring pattern binds; Name holds the binding for the simple
// case. Default is the initializer expression for `name = default`
// (applied only when the argument is undefined). Rest signals the
// trailing `...name` collector — only legal on the final param.
type Param struct {
	Name    string
	Default Node
	Rest    bool
	Pattern Pattern // non-nil for `function f({a, b})`
}

type FunctionDecl struct {
	Name        string
	Params      []Param
	Body        *Block
	IsAsync     bool
	IsGenerator bool
}

type FunctionExpr struct {
	Name        string // "" for anonymous
	Params      []Param
	Body        *Block
	IsAsync     bool
	IsGenerator bool
}

// ArrowFunctionExpr is `(params) => body` (or single-ident form
// `x => body`). When ExprBody is non-nil the body is a single
// expression whose value is returned; otherwise Body is a block.
type ArrowFunctionExpr struct {
	Params   []Param
	ExprBody Node   // non-nil if expression body
	Body     *Block // non-nil if block body
	IsAsync  bool
}

// AwaitExpr is `await x`. Compiler emits X then OpAwait, which the
// VM resolves by reading the promise's state synchronously — pending
// promises throw, since we have no real suspension.
type AwaitExpr struct{ X Node }

// YieldExpr is `yield expr` or just `yield`. Only legal inside a
// `function*` body. Compiler emits the value (or undefined) then
// OpYield, which suspends the generator and resumes when .next()
// is next called.
type YieldExpr struct {
	X        Node // may be nil
	Delegate bool // true for `yield* iterable` — NYI but reserved
}

type CallExpr struct {
	Callee Node
	Args   []Node
}

// NewExpr is `new Callee(args...)`. Parser produces this when a
// `new` keyword is followed by an expression and an optional call.
type NewExpr struct {
	Callee Node
	Args   []Node
}

// Expressions.

type NumberLit struct{ Value float64 }

// BigIntLit is `123n` / `0xFFn`. Digits is the cleaned literal (no
// underscores, no suffix), Base is 2/8/10/16.
type BigIntLit struct {
	Digits string
	Base   int
}
type StringLit struct{ Value string }
type BoolLit struct{ Value bool }
type NullLit struct{}
type UndefinedLit struct{}
type ThisExpr struct{}
type Ident struct{ Name string }

type UnaryExpr struct {
	Op string
	X  Node
}

// BinaryExpr is non-short-circuit (arithmetic + comparison). Logical
// && / || use LogicalExpr so the compiler emits short-circuit jumps.
type BinaryExpr struct {
	Op   string
	L, R Node
}

type LogicalExpr struct {
	Op   string // "&&", "||", "??"
	L, R Node
}

// ConditionalExpr is `test ? cons : alt`.
type ConditionalExpr struct {
	Test, Cons, Alt Node
}

// DoWhileStmt is `do body while (test);`.
type DoWhileStmt struct {
	Body Node
	Test Node
}

// OptionalMemberExpr is `obj?.prop`: undefined-passthrough access.
type OptionalMemberExpr struct {
	Obj  Node
	Prop string
}

// AssignExpr's Target is either *Ident or *MemberExpr. Compound ops
// (+=/-=/...) only support *Ident targets for now; *MemberExpr would
// require evaluating the object twice without re-running side effects.
type AssignExpr struct {
	Op     string // "=", "+=", "-=", "*=", "/="
	Target Node
	Value  Node
}

// ObjectProp pairs a property key (an identifier or string literal,
// already normalised to a plain string) with its value expression.
// ObjectProp represents one property in an object literal.
//   - Plain: {Key, Value} for `key: expr` (or shorthand `key`).
//   - Computed: Computed=true and KeyExpr holds the expression that
//     produces the key at runtime (`{[expr]: v}`).
//   - Spread: Spread=true; Value holds the source whose own enumerable
//     props get copied. Key / KeyExpr are ignored.
type ObjectProp struct {
	Key      string
	KeyExpr  Node
	Computed bool
	Value    Node
	Spread   bool
	Kind     string // "" / "get" / "set"
}

type ObjectLit struct{ Props []ObjectProp }

type MemberExpr struct {
	Obj  Node
	Prop string
}

type IndexExpr struct {
	Obj   Node
	Index Node
}

type ArrayLit struct{ Items []Node }

// ClassDecl is `class Name [extends Parent] { ...members }`.
// Members carry method bodies; the compiler lowers the whole thing
// to a constructor function plus per-method prototype assignments
// (and per-static ctor-property assignments).
type ClassDecl struct {
	Name    string
	Parent  Node // nil if no extends clause
	Members []ClassMember
}

// ClassMember is one method or constructor inside a class body.
type ClassMember struct {
	Name          string
	IsStatic      bool
	IsConstructor bool
	Kind          string // "" / "get" / "set"
	Params        []Param
	Body          *Block
}

// RegexLit is `/pat/flags`. The compiler lowers it to `new RegExp(pat, flags)`.
type RegexLit struct {
	Pattern string
	Flags   string
}

// TemplateLit is `` `prefix${e1}mid${e2}suffix` ``: Quasis is the list
// of string chunks (always one longer than Exprs); Exprs holds the
// interpolated expressions. Compiler lowers it to a String concat
// chain.
type TemplateLit struct {
	Quasis []string
	Exprs  []Node
}

// SpreadElement wraps `...expr` in array literals, call argument
// lists, and object literals. The consumer (compiler) inspects for
// SpreadElement to pick a per-item Push vs an iterate-and-Push path.
type SpreadElement struct{ Arg Node }

// RestElement is the destructuring / param counterpart: `...name`
// collects the remaining items/args into Name as an Array.
type RestElement struct{ Name string }

type UpdateExpr struct {
	Op     string // "++" or "--"
	Target *Ident
	Prefix bool
}

func (*Program) astNode()      {}
func (*ExprStmt) astNode()     {}
func (*LetDecl) astNode()      {}
func (*MultiVarDecl) astNode()    {}
func (*DestructureDecl) astNode() {}
func (*ObjectPattern) astNode()   {}
func (*ArrayPattern) astNode()    {}
func (*Block) astNode()        {}
func (*ForStmt) astNode()      {}
func (*ForOfStmt) astNode()    {}
func (*ForInStmt) astNode()    {}
func (*NumberLit) astNode()    {}
func (*BigIntLit) astNode()    {}
func (*StringLit) astNode()    {}
func (*BoolLit) astNode()      {}
func (*NullLit) astNode()      {}
func (*UndefinedLit) astNode() {}
func (*ThisExpr) astNode()     {}
func (*Ident) astNode()        {}
func (*UnaryExpr) astNode()    {}
func (*BinaryExpr) astNode()   {}
func (*LogicalExpr) astNode()        {}
func (*ConditionalExpr) astNode()    {}
func (*DoWhileStmt) astNode()        {}
func (*OptionalMemberExpr) astNode() {}
func (*AssignExpr) astNode()   {}
func (*UpdateExpr) astNode()   {}
func (*ObjectLit) astNode()    {}
func (*MemberExpr) astNode()   {}
func (*IfStmt) astNode()       {}
func (*ReturnStmt) astNode()   {}
func (*FunctionDecl) astNode() {}
func (*FunctionExpr) astNode() {}
func (*CallExpr) astNode()           {}
func (*ThrowStmt) astNode()          {}
func (*TryStmt) astNode()            {}
func (*WhileStmt) astNode()          {}
func (*BreakStmt) astNode()          {}
func (*ContinueStmt) astNode()       {}
func (*LabeledStmt) astNode()        {}
func (*SwitchStmt) astNode()         {}
func (*IndexExpr) astNode()          {}
func (*ArrayLit) astNode()           {}
func (*TemplateLit) astNode()        {}
func (*RegexLit) astNode()           {}
func (*ClassDecl) astNode()          {}
func (*SpreadElement) astNode()      {}
func (*RestElement) astNode()        {}
func (*ArrowFunctionExpr) astNode()  {}
func (*AwaitExpr) astNode()          {}
func (*YieldExpr) astNode()          {}
func (*NewExpr) astNode()            {}

