// Code generated from TaskOrchestrationParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package parser // TaskOrchestrationParser
import (
	"fmt"
	"strconv"
	"sync"

	"github.com/antlr4-go/antlr/v4"
)

// Suppress unused import errors
var (
	_ = fmt.Printf
	_ = strconv.Itoa
	_ = sync.Once{}
)

type TaskOrchestrationParser struct {
	*antlr.BaseParser
}

var TaskOrchestrationParserParserStaticData struct {
	once                   sync.Once
	serializedATN          []int32
	LiteralNames           []string
	SymbolicNames          []string
	RuleNames              []string
	PredictionContextCache *antlr.PredictionContextCache
	atn                    *antlr.ATN
	decisionToDFA          []*antlr.DFA
}

func taskorchestrationparserParserInit() {
	staticData := &TaskOrchestrationParserParserStaticData
	staticData.LiteralNames = []string{
		"", "", "'->'", "'&&'", "'||'", "'?'", "':'", "'*'", "'('", "')'", "'['",
		"']'", "'{'", "'}'", "','", "", "", "';'",
	}
	staticData.SymbolicNames = []string{
		"", "TASK_NAME", "ARROW", "AND", "OR", "QUESTION", "COLON", "STAR",
		"LPAREN", "RPAREN", "LBRACKET", "RBRACKET", "LBRACE", "RBRACE", "COMMA",
		"WS", "COMMENT", "SEMICOLON", "NEWLINE",
	}
	staticData.RuleNames = []string{
		"program", "expression", "orExpression", "andExpression", "sequenceExpression",
		"conditionalExpression", "repetitionExpression", "primaryExpression",
		"task", "parallelGroup", "joinGroup",
	}
	staticData.PredictionContextCache = antlr.NewPredictionContextCache()
	staticData.serializedATN = []int32{
		4, 1, 18, 108, 2, 0, 7, 0, 2, 1, 7, 1, 2, 2, 7, 2, 2, 3, 7, 3, 2, 4, 7,
		4, 2, 5, 7, 5, 2, 6, 7, 6, 2, 7, 7, 7, 2, 8, 7, 8, 2, 9, 7, 9, 2, 10, 7,
		10, 1, 0, 1, 0, 5, 0, 25, 8, 0, 10, 0, 12, 0, 28, 9, 0, 4, 0, 30, 8, 0,
		11, 0, 12, 0, 31, 1, 0, 1, 0, 1, 1, 1, 1, 1, 2, 1, 2, 1, 2, 5, 2, 41, 8,
		2, 10, 2, 12, 2, 44, 9, 2, 1, 3, 1, 3, 1, 3, 5, 3, 49, 8, 3, 10, 3, 12,
		3, 52, 9, 3, 1, 4, 1, 4, 1, 4, 5, 4, 57, 8, 4, 10, 4, 12, 4, 60, 9, 4,
		1, 5, 1, 5, 1, 5, 1, 5, 1, 5, 1, 5, 3, 5, 68, 8, 5, 1, 6, 1, 6, 3, 6, 72,
		8, 6, 1, 7, 1, 7, 1, 7, 1, 7, 1, 7, 3, 7, 79, 8, 7, 1, 8, 1, 8, 1, 8, 3,
		8, 84, 8, 8, 1, 9, 1, 9, 1, 9, 1, 9, 5, 9, 90, 8, 9, 10, 9, 12, 9, 93,
		9, 9, 1, 9, 1, 9, 1, 10, 1, 10, 1, 10, 1, 10, 5, 10, 101, 8, 10, 10, 10,
		12, 10, 104, 9, 10, 1, 10, 1, 10, 1, 10, 0, 0, 11, 0, 2, 4, 6, 8, 10, 12,
		14, 16, 18, 20, 0, 1, 1, 0, 17, 18, 108, 0, 29, 1, 0, 0, 0, 2, 35, 1, 0,
		0, 0, 4, 37, 1, 0, 0, 0, 6, 45, 1, 0, 0, 0, 8, 53, 1, 0, 0, 0, 10, 61,
		1, 0, 0, 0, 12, 69, 1, 0, 0, 0, 14, 78, 1, 0, 0, 0, 16, 83, 1, 0, 0, 0,
		18, 85, 1, 0, 0, 0, 20, 96, 1, 0, 0, 0, 22, 26, 3, 2, 1, 0, 23, 25, 7,
		0, 0, 0, 24, 23, 1, 0, 0, 0, 25, 28, 1, 0, 0, 0, 26, 24, 1, 0, 0, 0, 26,
		27, 1, 0, 0, 0, 27, 30, 1, 0, 0, 0, 28, 26, 1, 0, 0, 0, 29, 22, 1, 0, 0,
		0, 30, 31, 1, 0, 0, 0, 31, 29, 1, 0, 0, 0, 31, 32, 1, 0, 0, 0, 32, 33,
		1, 0, 0, 0, 33, 34, 5, 0, 0, 1, 34, 1, 1, 0, 0, 0, 35, 36, 3, 4, 2, 0,
		36, 3, 1, 0, 0, 0, 37, 42, 3, 6, 3, 0, 38, 39, 5, 4, 0, 0, 39, 41, 3, 6,
		3, 0, 40, 38, 1, 0, 0, 0, 41, 44, 1, 0, 0, 0, 42, 40, 1, 0, 0, 0, 42, 43,
		1, 0, 0, 0, 43, 5, 1, 0, 0, 0, 44, 42, 1, 0, 0, 0, 45, 50, 3, 8, 4, 0,
		46, 47, 5, 3, 0, 0, 47, 49, 3, 8, 4, 0, 48, 46, 1, 0, 0, 0, 49, 52, 1,
		0, 0, 0, 50, 48, 1, 0, 0, 0, 50, 51, 1, 0, 0, 0, 51, 7, 1, 0, 0, 0, 52,
		50, 1, 0, 0, 0, 53, 58, 3, 10, 5, 0, 54, 55, 5, 2, 0, 0, 55, 57, 3, 10,
		5, 0, 56, 54, 1, 0, 0, 0, 57, 60, 1, 0, 0, 0, 58, 56, 1, 0, 0, 0, 58, 59,
		1, 0, 0, 0, 59, 9, 1, 0, 0, 0, 60, 58, 1, 0, 0, 0, 61, 67, 3, 12, 6, 0,
		62, 63, 5, 5, 0, 0, 63, 64, 3, 2, 1, 0, 64, 65, 5, 6, 0, 0, 65, 66, 3,
		2, 1, 0, 66, 68, 1, 0, 0, 0, 67, 62, 1, 0, 0, 0, 67, 68, 1, 0, 0, 0, 68,
		11, 1, 0, 0, 0, 69, 71, 3, 14, 7, 0, 70, 72, 5, 7, 0, 0, 71, 70, 1, 0,
		0, 0, 71, 72, 1, 0, 0, 0, 72, 13, 1, 0, 0, 0, 73, 79, 3, 16, 8, 0, 74,
		75, 5, 8, 0, 0, 75, 76, 3, 2, 1, 0, 76, 77, 5, 9, 0, 0, 77, 79, 1, 0, 0,
		0, 78, 73, 1, 0, 0, 0, 78, 74, 1, 0, 0, 0, 79, 15, 1, 0, 0, 0, 80, 84,
		5, 1, 0, 0, 81, 84, 3, 18, 9, 0, 82, 84, 3, 20, 10, 0, 83, 80, 1, 0, 0,
		0, 83, 81, 1, 0, 0, 0, 83, 82, 1, 0, 0, 0, 84, 17, 1, 0, 0, 0, 85, 86,
		5, 10, 0, 0, 86, 91, 3, 16, 8, 0, 87, 88, 5, 14, 0, 0, 88, 90, 3, 16, 8,
		0, 89, 87, 1, 0, 0, 0, 90, 93, 1, 0, 0, 0, 91, 89, 1, 0, 0, 0, 91, 92,
		1, 0, 0, 0, 92, 94, 1, 0, 0, 0, 93, 91, 1, 0, 0, 0, 94, 95, 5, 11, 0, 0,
		95, 19, 1, 0, 0, 0, 96, 97, 5, 12, 0, 0, 97, 102, 3, 16, 8, 0, 98, 99,
		5, 14, 0, 0, 99, 101, 3, 16, 8, 0, 100, 98, 1, 0, 0, 0, 101, 104, 1, 0,
		0, 0, 102, 100, 1, 0, 0, 0, 102, 103, 1, 0, 0, 0, 103, 105, 1, 0, 0, 0,
		104, 102, 1, 0, 0, 0, 105, 106, 5, 13, 0, 0, 106, 21, 1, 0, 0, 0, 11, 26,
		31, 42, 50, 58, 67, 71, 78, 83, 91, 102,
	}
	deserializer := antlr.NewATNDeserializer(nil)
	staticData.atn = deserializer.Deserialize(staticData.serializedATN)
	atn := staticData.atn
	staticData.decisionToDFA = make([]*antlr.DFA, len(atn.DecisionToState))
	decisionToDFA := staticData.decisionToDFA
	for index, state := range atn.DecisionToState {
		decisionToDFA[index] = antlr.NewDFA(state, index)
	}
}

// TaskOrchestrationParserInit initializes any static state used to implement TaskOrchestrationParser. By default the
// static state used to implement the parser is lazily initialized during the first call to
// NewTaskOrchestrationParser(). You can call this function if you wish to initialize the static state ahead
// of time.
func TaskOrchestrationParserInit() {
	staticData := &TaskOrchestrationParserParserStaticData
	staticData.once.Do(taskorchestrationparserParserInit)
}

// NewTaskOrchestrationParser produces a new parser instance for the optional input antlr.TokenStream.
func NewTaskOrchestrationParser(input antlr.TokenStream) *TaskOrchestrationParser {
	TaskOrchestrationParserInit()
	this := new(TaskOrchestrationParser)
	this.BaseParser = antlr.NewBaseParser(input)
	staticData := &TaskOrchestrationParserParserStaticData
	this.Interpreter = antlr.NewParserATNSimulator(this, staticData.atn, staticData.decisionToDFA, staticData.PredictionContextCache)
	this.RuleNames = staticData.RuleNames
	this.LiteralNames = staticData.LiteralNames
	this.SymbolicNames = staticData.SymbolicNames
	this.GrammarFileName = "TaskOrchestrationParser.g4"

	return this
}

// TaskOrchestrationParser tokens.
const (
	TaskOrchestrationParserEOF       = antlr.TokenEOF
	TaskOrchestrationParserTASK_NAME = 1
	TaskOrchestrationParserARROW     = 2
	TaskOrchestrationParserAND       = 3
	TaskOrchestrationParserOR        = 4
	TaskOrchestrationParserQUESTION  = 5
	TaskOrchestrationParserCOLON     = 6
	TaskOrchestrationParserSTAR      = 7
	TaskOrchestrationParserLPAREN    = 8
	TaskOrchestrationParserRPAREN    = 9
	TaskOrchestrationParserLBRACKET  = 10
	TaskOrchestrationParserRBRACKET  = 11
	TaskOrchestrationParserLBRACE    = 12
	TaskOrchestrationParserRBRACE    = 13
	TaskOrchestrationParserCOMMA     = 14
	TaskOrchestrationParserWS        = 15
	TaskOrchestrationParserCOMMENT   = 16
	TaskOrchestrationParserSEMICOLON = 17
	TaskOrchestrationParserNEWLINE   = 18
)

// TaskOrchestrationParser rules.
const (
	TaskOrchestrationParserRULE_program               = 0
	TaskOrchestrationParserRULE_expression            = 1
	TaskOrchestrationParserRULE_orExpression          = 2
	TaskOrchestrationParserRULE_andExpression         = 3
	TaskOrchestrationParserRULE_sequenceExpression    = 4
	TaskOrchestrationParserRULE_conditionalExpression = 5
	TaskOrchestrationParserRULE_repetitionExpression  = 6
	TaskOrchestrationParserRULE_primaryExpression     = 7
	TaskOrchestrationParserRULE_task                  = 8
	TaskOrchestrationParserRULE_parallelGroup         = 9
	TaskOrchestrationParserRULE_joinGroup             = 10
)

// IProgramContext is an interface to support dynamic dispatch.
type IProgramContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	EOF() antlr.TerminalNode
	AllExpression() []IExpressionContext
	Expression(i int) IExpressionContext
	AllSEMICOLON() []antlr.TerminalNode
	SEMICOLON(i int) antlr.TerminalNode
	AllNEWLINE() []antlr.TerminalNode
	NEWLINE(i int) antlr.TerminalNode

	// IsProgramContext differentiates from other interfaces.
	IsProgramContext()
}

type ProgramContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyProgramContext() *ProgramContext {
	p := new(ProgramContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_program
	return p
}

func InitEmptyProgramContext(p *ProgramContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_program
}

func (*ProgramContext) IsProgramContext() {}

func NewProgramContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ProgramContext {
	p := new(ProgramContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_program

	return p
}

func (s *ProgramContext) GetParser() antlr.Parser { return s.parser }

func (s *ProgramContext) EOF() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserEOF, 0)
}

func (s *ProgramContext) AllExpression() []IExpressionContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExpressionContext); ok {
			len++
		}
	}

	tst := make([]IExpressionContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExpressionContext); ok {
			tst[i] = t.(IExpressionContext)
			i++
		}
	}

	return tst
}

func (s *ProgramContext) Expression(i int) IExpressionContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExpressionContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExpressionContext)
}

func (s *ProgramContext) AllSEMICOLON() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserSEMICOLON)
}

func (s *ProgramContext) SEMICOLON(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserSEMICOLON, i)
}

func (s *ProgramContext) AllNEWLINE() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserNEWLINE)
}

func (s *ProgramContext) NEWLINE(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserNEWLINE, i)
}

func (s *ProgramContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ProgramContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ProgramContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterProgram(s)
	}
}

func (s *ProgramContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitProgram(s)
	}
}

func (s *ProgramContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitProgram(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) Program() (localctx IProgramContext) {
	localctx = NewProgramContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 0, TaskOrchestrationParserRULE_program)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	p.SetState(29)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for ok := true; ok; ok = ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&5378) != 0) {
		{
			p.SetState(22)
			p.Expression()
		}
		p.SetState(26)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		for _la == TaskOrchestrationParserSEMICOLON || _la == TaskOrchestrationParserNEWLINE {
			{
				p.SetState(23)
				_la = p.GetTokenStream().LA(1)

				if !(_la == TaskOrchestrationParserSEMICOLON || _la == TaskOrchestrationParserNEWLINE) {
					p.GetErrorHandler().RecoverInline(p)
				} else {
					p.GetErrorHandler().ReportMatch(p)
					p.Consume()
				}
			}

			p.SetState(28)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}
			_la = p.GetTokenStream().LA(1)
		}

		p.SetState(31)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	{
		p.SetState(33)
		p.Match(TaskOrchestrationParserEOF)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IExpressionContext is an interface to support dynamic dispatch.
type IExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	OrExpression() IOrExpressionContext

	// IsExpressionContext differentiates from other interfaces.
	IsExpressionContext()
}

type ExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyExpressionContext() *ExpressionContext {
	p := new(ExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_expression
	return p
}

func InitEmptyExpressionContext(p *ExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_expression
}

func (*ExpressionContext) IsExpressionContext() {}

func NewExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ExpressionContext {
	p := new(ExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_expression

	return p
}

func (s *ExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *ExpressionContext) OrExpression() IOrExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IOrExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IOrExpressionContext)
}

func (s *ExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterExpression(s)
	}
}

func (s *ExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitExpression(s)
	}
}

func (s *ExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) Expression() (localctx IExpressionContext) {
	localctx = NewExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 2, TaskOrchestrationParserRULE_expression)
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(35)
		p.OrExpression()
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IOrExpressionContext is an interface to support dynamic dispatch.
type IOrExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllAndExpression() []IAndExpressionContext
	AndExpression(i int) IAndExpressionContext
	AllOR() []antlr.TerminalNode
	OR(i int) antlr.TerminalNode

	// IsOrExpressionContext differentiates from other interfaces.
	IsOrExpressionContext()
}

type OrExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyOrExpressionContext() *OrExpressionContext {
	p := new(OrExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_orExpression
	return p
}

func InitEmptyOrExpressionContext(p *OrExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_orExpression
}

func (*OrExpressionContext) IsOrExpressionContext() {}

func NewOrExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *OrExpressionContext {
	p := new(OrExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_orExpression

	return p
}

func (s *OrExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *OrExpressionContext) AllAndExpression() []IAndExpressionContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IAndExpressionContext); ok {
			len++
		}
	}

	tst := make([]IAndExpressionContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IAndExpressionContext); ok {
			tst[i] = t.(IAndExpressionContext)
			i++
		}
	}

	return tst
}

func (s *OrExpressionContext) AndExpression(i int) IAndExpressionContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAndExpressionContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAndExpressionContext)
}

func (s *OrExpressionContext) AllOR() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserOR)
}

func (s *OrExpressionContext) OR(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserOR, i)
}

func (s *OrExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *OrExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *OrExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterOrExpression(s)
	}
}

func (s *OrExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitOrExpression(s)
	}
}

func (s *OrExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitOrExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) OrExpression() (localctx IOrExpressionContext) {
	localctx = NewOrExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 4, TaskOrchestrationParserRULE_orExpression)
	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(37)
		p.AndExpression()
	}
	p.SetState(42)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 2, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			{
				p.SetState(38)
				p.Match(TaskOrchestrationParserOR)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(39)
				p.AndExpression()
			}

		}
		p.SetState(44)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 2, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IAndExpressionContext is an interface to support dynamic dispatch.
type IAndExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllSequenceExpression() []ISequenceExpressionContext
	SequenceExpression(i int) ISequenceExpressionContext
	AllAND() []antlr.TerminalNode
	AND(i int) antlr.TerminalNode

	// IsAndExpressionContext differentiates from other interfaces.
	IsAndExpressionContext()
}

type AndExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyAndExpressionContext() *AndExpressionContext {
	p := new(AndExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_andExpression
	return p
}

func InitEmptyAndExpressionContext(p *AndExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_andExpression
}

func (*AndExpressionContext) IsAndExpressionContext() {}

func NewAndExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *AndExpressionContext {
	p := new(AndExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_andExpression

	return p
}

func (s *AndExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *AndExpressionContext) AllSequenceExpression() []ISequenceExpressionContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ISequenceExpressionContext); ok {
			len++
		}
	}

	tst := make([]ISequenceExpressionContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ISequenceExpressionContext); ok {
			tst[i] = t.(ISequenceExpressionContext)
			i++
		}
	}

	return tst
}

func (s *AndExpressionContext) SequenceExpression(i int) ISequenceExpressionContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ISequenceExpressionContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ISequenceExpressionContext)
}

func (s *AndExpressionContext) AllAND() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserAND)
}

func (s *AndExpressionContext) AND(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserAND, i)
}

func (s *AndExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *AndExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *AndExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterAndExpression(s)
	}
}

func (s *AndExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitAndExpression(s)
	}
}

func (s *AndExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitAndExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) AndExpression() (localctx IAndExpressionContext) {
	localctx = NewAndExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 6, TaskOrchestrationParserRULE_andExpression)
	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(45)
		p.SequenceExpression()
	}
	p.SetState(50)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 3, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			{
				p.SetState(46)
				p.Match(TaskOrchestrationParserAND)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(47)
				p.SequenceExpression()
			}

		}
		p.SetState(52)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 3, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ISequenceExpressionContext is an interface to support dynamic dispatch.
type ISequenceExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllConditionalExpression() []IConditionalExpressionContext
	ConditionalExpression(i int) IConditionalExpressionContext
	AllARROW() []antlr.TerminalNode
	ARROW(i int) antlr.TerminalNode

	// IsSequenceExpressionContext differentiates from other interfaces.
	IsSequenceExpressionContext()
}

type SequenceExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptySequenceExpressionContext() *SequenceExpressionContext {
	p := new(SequenceExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_sequenceExpression
	return p
}

func InitEmptySequenceExpressionContext(p *SequenceExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_sequenceExpression
}

func (*SequenceExpressionContext) IsSequenceExpressionContext() {}

func NewSequenceExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *SequenceExpressionContext {
	p := new(SequenceExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_sequenceExpression

	return p
}

func (s *SequenceExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *SequenceExpressionContext) AllConditionalExpression() []IConditionalExpressionContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IConditionalExpressionContext); ok {
			len++
		}
	}

	tst := make([]IConditionalExpressionContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IConditionalExpressionContext); ok {
			tst[i] = t.(IConditionalExpressionContext)
			i++
		}
	}

	return tst
}

func (s *SequenceExpressionContext) ConditionalExpression(i int) IConditionalExpressionContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IConditionalExpressionContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IConditionalExpressionContext)
}

func (s *SequenceExpressionContext) AllARROW() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserARROW)
}

func (s *SequenceExpressionContext) ARROW(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserARROW, i)
}

func (s *SequenceExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *SequenceExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *SequenceExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterSequenceExpression(s)
	}
}

func (s *SequenceExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitSequenceExpression(s)
	}
}

func (s *SequenceExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitSequenceExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) SequenceExpression() (localctx ISequenceExpressionContext) {
	localctx = NewSequenceExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 8, TaskOrchestrationParserRULE_sequenceExpression)
	var _alt int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(53)
		p.ConditionalExpression()
	}
	p.SetState(58)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 4, p.GetParserRuleContext())
	if p.HasError() {
		goto errorExit
	}
	for _alt != 2 && _alt != antlr.ATNInvalidAltNumber {
		if _alt == 1 {
			{
				p.SetState(54)
				p.Match(TaskOrchestrationParserARROW)
				if p.HasError() {
					// Recognition error - abort rule
					goto errorExit
				}
			}
			{
				p.SetState(55)
				p.ConditionalExpression()
			}

		}
		p.SetState(60)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_alt = p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 4, p.GetParserRuleContext())
		if p.HasError() {
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IConditionalExpressionContext is an interface to support dynamic dispatch.
type IConditionalExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	RepetitionExpression() IRepetitionExpressionContext
	QUESTION() antlr.TerminalNode
	AllExpression() []IExpressionContext
	Expression(i int) IExpressionContext
	COLON() antlr.TerminalNode

	// IsConditionalExpressionContext differentiates from other interfaces.
	IsConditionalExpressionContext()
}

type ConditionalExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyConditionalExpressionContext() *ConditionalExpressionContext {
	p := new(ConditionalExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_conditionalExpression
	return p
}

func InitEmptyConditionalExpressionContext(p *ConditionalExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_conditionalExpression
}

func (*ConditionalExpressionContext) IsConditionalExpressionContext() {}

func NewConditionalExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ConditionalExpressionContext {
	p := new(ConditionalExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_conditionalExpression

	return p
}

func (s *ConditionalExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *ConditionalExpressionContext) RepetitionExpression() IRepetitionExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IRepetitionExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IRepetitionExpressionContext)
}

func (s *ConditionalExpressionContext) QUESTION() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserQUESTION, 0)
}

func (s *ConditionalExpressionContext) AllExpression() []IExpressionContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IExpressionContext); ok {
			len++
		}
	}

	tst := make([]IExpressionContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IExpressionContext); ok {
			tst[i] = t.(IExpressionContext)
			i++
		}
	}

	return tst
}

func (s *ConditionalExpressionContext) Expression(i int) IExpressionContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExpressionContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExpressionContext)
}

func (s *ConditionalExpressionContext) COLON() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserCOLON, 0)
}

func (s *ConditionalExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ConditionalExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ConditionalExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterConditionalExpression(s)
	}
}

func (s *ConditionalExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitConditionalExpression(s)
	}
}

func (s *ConditionalExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitConditionalExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) ConditionalExpression() (localctx IConditionalExpressionContext) {
	localctx = NewConditionalExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 10, TaskOrchestrationParserRULE_conditionalExpression)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(61)
		p.RepetitionExpression()
	}
	p.SetState(67)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == TaskOrchestrationParserQUESTION {
		{
			p.SetState(62)
			p.Match(TaskOrchestrationParserQUESTION)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(63)
			p.Expression()
		}
		{
			p.SetState(64)
			p.Match(TaskOrchestrationParserCOLON)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(65)
			p.Expression()
		}

	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IRepetitionExpressionContext is an interface to support dynamic dispatch.
type IRepetitionExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	PrimaryExpression() IPrimaryExpressionContext
	STAR() antlr.TerminalNode

	// IsRepetitionExpressionContext differentiates from other interfaces.
	IsRepetitionExpressionContext()
}

type RepetitionExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyRepetitionExpressionContext() *RepetitionExpressionContext {
	p := new(RepetitionExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_repetitionExpression
	return p
}

func InitEmptyRepetitionExpressionContext(p *RepetitionExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_repetitionExpression
}

func (*RepetitionExpressionContext) IsRepetitionExpressionContext() {}

func NewRepetitionExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *RepetitionExpressionContext {
	p := new(RepetitionExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_repetitionExpression

	return p
}

func (s *RepetitionExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *RepetitionExpressionContext) PrimaryExpression() IPrimaryExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IPrimaryExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IPrimaryExpressionContext)
}

func (s *RepetitionExpressionContext) STAR() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserSTAR, 0)
}

func (s *RepetitionExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *RepetitionExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *RepetitionExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterRepetitionExpression(s)
	}
}

func (s *RepetitionExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitRepetitionExpression(s)
	}
}

func (s *RepetitionExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitRepetitionExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) RepetitionExpression() (localctx IRepetitionExpressionContext) {
	localctx = NewRepetitionExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 12, TaskOrchestrationParserRULE_repetitionExpression)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(69)
		p.PrimaryExpression()
	}
	p.SetState(71)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	if _la == TaskOrchestrationParserSTAR {
		{
			p.SetState(70)
			p.Match(TaskOrchestrationParserSTAR)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IPrimaryExpressionContext is an interface to support dynamic dispatch.
type IPrimaryExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	Task() ITaskContext
	LPAREN() antlr.TerminalNode
	Expression() IExpressionContext
	RPAREN() antlr.TerminalNode

	// IsPrimaryExpressionContext differentiates from other interfaces.
	IsPrimaryExpressionContext()
}

type PrimaryExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyPrimaryExpressionContext() *PrimaryExpressionContext {
	p := new(PrimaryExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_primaryExpression
	return p
}

func InitEmptyPrimaryExpressionContext(p *PrimaryExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_primaryExpression
}

func (*PrimaryExpressionContext) IsPrimaryExpressionContext() {}

func NewPrimaryExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *PrimaryExpressionContext {
	p := new(PrimaryExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_primaryExpression

	return p
}

func (s *PrimaryExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *PrimaryExpressionContext) Task() ITaskContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ITaskContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ITaskContext)
}

func (s *PrimaryExpressionContext) LPAREN() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserLPAREN, 0)
}

func (s *PrimaryExpressionContext) Expression() IExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IExpressionContext)
}

func (s *PrimaryExpressionContext) RPAREN() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserRPAREN, 0)
}

func (s *PrimaryExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *PrimaryExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *PrimaryExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterPrimaryExpression(s)
	}
}

func (s *PrimaryExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitPrimaryExpression(s)
	}
}

func (s *PrimaryExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitPrimaryExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) PrimaryExpression() (localctx IPrimaryExpressionContext) {
	localctx = NewPrimaryExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 14, TaskOrchestrationParserRULE_primaryExpression)
	p.SetState(78)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case TaskOrchestrationParserTASK_NAME, TaskOrchestrationParserLBRACKET, TaskOrchestrationParserLBRACE:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(73)
			p.Task()
		}

	case TaskOrchestrationParserLPAREN:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(74)
			p.Match(TaskOrchestrationParserLPAREN)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(75)
			p.Expression()
		}
		{
			p.SetState(76)
			p.Match(TaskOrchestrationParserRPAREN)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// ITaskContext is an interface to support dynamic dispatch.
type ITaskContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	TASK_NAME() antlr.TerminalNode
	ParallelGroup() IParallelGroupContext
	JoinGroup() IJoinGroupContext

	// IsTaskContext differentiates from other interfaces.
	IsTaskContext()
}

type TaskContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyTaskContext() *TaskContext {
	p := new(TaskContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_task
	return p
}

func InitEmptyTaskContext(p *TaskContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_task
}

func (*TaskContext) IsTaskContext() {}

func NewTaskContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *TaskContext {
	p := new(TaskContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_task

	return p
}

func (s *TaskContext) GetParser() antlr.Parser { return s.parser }

func (s *TaskContext) TASK_NAME() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserTASK_NAME, 0)
}

func (s *TaskContext) ParallelGroup() IParallelGroupContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IParallelGroupContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IParallelGroupContext)
}

func (s *TaskContext) JoinGroup() IJoinGroupContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IJoinGroupContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IJoinGroupContext)
}

func (s *TaskContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *TaskContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *TaskContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterTask(s)
	}
}

func (s *TaskContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitTask(s)
	}
}

func (s *TaskContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitTask(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) Task() (localctx ITaskContext) {
	localctx = NewTaskContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 16, TaskOrchestrationParserRULE_task)
	p.SetState(83)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case TaskOrchestrationParserTASK_NAME:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(80)
			p.Match(TaskOrchestrationParserTASK_NAME)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case TaskOrchestrationParserLBRACKET:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(81)
			p.ParallelGroup()
		}

	case TaskOrchestrationParserLBRACE:
		p.EnterOuterAlt(localctx, 3)
		{
			p.SetState(82)
			p.JoinGroup()
		}

	default:
		p.SetError(antlr.NewNoViableAltException(p, nil, nil, nil, nil, nil))
		goto errorExit
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IParallelGroupContext is an interface to support dynamic dispatch.
type IParallelGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	LBRACKET() antlr.TerminalNode
	AllTask() []ITaskContext
	Task(i int) ITaskContext
	RBRACKET() antlr.TerminalNode
	AllCOMMA() []antlr.TerminalNode
	COMMA(i int) antlr.TerminalNode

	// IsParallelGroupContext differentiates from other interfaces.
	IsParallelGroupContext()
}

type ParallelGroupContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyParallelGroupContext() *ParallelGroupContext {
	p := new(ParallelGroupContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_parallelGroup
	return p
}

func InitEmptyParallelGroupContext(p *ParallelGroupContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_parallelGroup
}

func (*ParallelGroupContext) IsParallelGroupContext() {}

func NewParallelGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *ParallelGroupContext {
	p := new(ParallelGroupContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_parallelGroup

	return p
}

func (s *ParallelGroupContext) GetParser() antlr.Parser { return s.parser }

func (s *ParallelGroupContext) LBRACKET() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserLBRACKET, 0)
}

func (s *ParallelGroupContext) AllTask() []ITaskContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ITaskContext); ok {
			len++
		}
	}

	tst := make([]ITaskContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ITaskContext); ok {
			tst[i] = t.(ITaskContext)
			i++
		}
	}

	return tst
}

func (s *ParallelGroupContext) Task(i int) ITaskContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ITaskContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ITaskContext)
}

func (s *ParallelGroupContext) RBRACKET() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserRBRACKET, 0)
}

func (s *ParallelGroupContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserCOMMA)
}

func (s *ParallelGroupContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserCOMMA, i)
}

func (s *ParallelGroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *ParallelGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *ParallelGroupContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterParallelGroup(s)
	}
}

func (s *ParallelGroupContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitParallelGroup(s)
	}
}

func (s *ParallelGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitParallelGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) ParallelGroup() (localctx IParallelGroupContext) {
	localctx = NewParallelGroupContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 18, TaskOrchestrationParserRULE_parallelGroup)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(85)
		p.Match(TaskOrchestrationParserLBRACKET)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(86)
		p.Task()
	}
	p.SetState(91)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == TaskOrchestrationParserCOMMA {
		{
			p.SetState(87)
			p.Match(TaskOrchestrationParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(88)
			p.Task()
		}

		p.SetState(93)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	{
		p.SetState(94)
		p.Match(TaskOrchestrationParserRBRACKET)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}

// IJoinGroupContext is an interface to support dynamic dispatch.
type IJoinGroupContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	LBRACE() antlr.TerminalNode
	AllTask() []ITaskContext
	Task(i int) ITaskContext
	RBRACE() antlr.TerminalNode
	AllCOMMA() []antlr.TerminalNode
	COMMA(i int) antlr.TerminalNode

	// IsJoinGroupContext differentiates from other interfaces.
	IsJoinGroupContext()
}

type JoinGroupContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyJoinGroupContext() *JoinGroupContext {
	p := new(JoinGroupContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_joinGroup
	return p
}

func InitEmptyJoinGroupContext(p *JoinGroupContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_joinGroup
}

func (*JoinGroupContext) IsJoinGroupContext() {}

func NewJoinGroupContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *JoinGroupContext {
	p := new(JoinGroupContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_joinGroup

	return p
}

func (s *JoinGroupContext) GetParser() antlr.Parser { return s.parser }

func (s *JoinGroupContext) LBRACE() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserLBRACE, 0)
}

func (s *JoinGroupContext) AllTask() []ITaskContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(ITaskContext); ok {
			len++
		}
	}

	tst := make([]ITaskContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(ITaskContext); ok {
			tst[i] = t.(ITaskContext)
			i++
		}
	}

	return tst
}

func (s *JoinGroupContext) Task(i int) ITaskContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ITaskContext); ok {
			if j == i {
				t = ctx.(antlr.RuleContext)
				break
			}
			j++
		}
	}

	if t == nil {
		return nil
	}

	return t.(ITaskContext)
}

func (s *JoinGroupContext) RBRACE() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserRBRACE, 0)
}

func (s *JoinGroupContext) AllCOMMA() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserCOMMA)
}

func (s *JoinGroupContext) COMMA(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserCOMMA, i)
}

func (s *JoinGroupContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *JoinGroupContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *JoinGroupContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterJoinGroup(s)
	}
}

func (s *JoinGroupContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitJoinGroup(s)
	}
}

func (s *JoinGroupContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitJoinGroup(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) JoinGroup() (localctx IJoinGroupContext) {
	localctx = NewJoinGroupContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 20, TaskOrchestrationParserRULE_joinGroup)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(96)
		p.Match(TaskOrchestrationParserLBRACE)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(97)
		p.Task()
	}
	p.SetState(102)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == TaskOrchestrationParserCOMMA {
		{
			p.SetState(98)
			p.Match(TaskOrchestrationParserCOMMA)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(99)
			p.Task()
		}

		p.SetState(104)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	{
		p.SetState(105)
		p.Match(TaskOrchestrationParserRBRACE)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}

errorExit:
	if p.HasError() {
		v := p.GetError()
		localctx.SetException(v)
		p.GetErrorHandler().ReportError(p, v)
		p.GetErrorHandler().Recover(p, v)
		p.SetError(nil)
	}
	p.ExitRule()
	return localctx
	goto errorExit // Trick to prevent compiler error if the label is not used
}
