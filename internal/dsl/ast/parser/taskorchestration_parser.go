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
		"conditionalExpression", "basicExpression", "task", "joinGroup",
	}
	staticData.PredictionContextCache = antlr.NewPredictionContextCache()
	staticData.serializedATN = []int32{
		4, 1, 18, 95, 2, 0, 7, 0, 2, 1, 7, 1, 2, 2, 7, 2, 2, 3, 7, 3, 2, 4, 7,
		4, 2, 5, 7, 5, 2, 6, 7, 6, 2, 7, 7, 7, 2, 8, 7, 8, 1, 0, 1, 0, 5, 0, 21,
		8, 0, 10, 0, 12, 0, 24, 9, 0, 4, 0, 26, 8, 0, 11, 0, 12, 0, 27, 1, 0, 1,
		0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 3, 1, 37, 8, 1, 1, 2, 1, 2, 1, 2, 1, 2,
		1, 2, 5, 2, 44, 8, 2, 10, 2, 12, 2, 47, 9, 2, 1, 3, 1, 3, 1, 3, 1, 3, 1,
		3, 5, 3, 54, 8, 3, 10, 3, 12, 3, 57, 9, 3, 1, 4, 1, 4, 1, 4, 4, 4, 62,
		8, 4, 11, 4, 12, 4, 63, 1, 5, 1, 5, 1, 5, 1, 5, 1, 5, 1, 5, 1, 6, 1, 6,
		1, 6, 1, 6, 1, 6, 1, 6, 3, 6, 78, 8, 6, 1, 7, 1, 7, 3, 7, 82, 8, 7, 1,
		8, 1, 8, 1, 8, 1, 8, 5, 8, 88, 8, 8, 10, 8, 12, 8, 91, 9, 8, 1, 8, 1, 8,
		1, 8, 0, 0, 9, 0, 2, 4, 6, 8, 10, 12, 14, 16, 0, 1, 1, 0, 17, 18, 98, 0,
		25, 1, 0, 0, 0, 2, 36, 1, 0, 0, 0, 4, 38, 1, 0, 0, 0, 6, 48, 1, 0, 0, 0,
		8, 58, 1, 0, 0, 0, 10, 65, 1, 0, 0, 0, 12, 77, 1, 0, 0, 0, 14, 81, 1, 0,
		0, 0, 16, 83, 1, 0, 0, 0, 18, 22, 3, 2, 1, 0, 19, 21, 7, 0, 0, 0, 20, 19,
		1, 0, 0, 0, 21, 24, 1, 0, 0, 0, 22, 20, 1, 0, 0, 0, 22, 23, 1, 0, 0, 0,
		23, 26, 1, 0, 0, 0, 24, 22, 1, 0, 0, 0, 25, 18, 1, 0, 0, 0, 26, 27, 1,
		0, 0, 0, 27, 25, 1, 0, 0, 0, 27, 28, 1, 0, 0, 0, 28, 29, 1, 0, 0, 0, 29,
		30, 5, 0, 0, 1, 30, 1, 1, 0, 0, 0, 31, 37, 3, 8, 4, 0, 32, 37, 3, 4, 2,
		0, 33, 37, 3, 6, 3, 0, 34, 37, 3, 10, 5, 0, 35, 37, 3, 12, 6, 0, 36, 31,
		1, 0, 0, 0, 36, 32, 1, 0, 0, 0, 36, 33, 1, 0, 0, 0, 36, 34, 1, 0, 0, 0,
		36, 35, 1, 0, 0, 0, 37, 3, 1, 0, 0, 0, 38, 39, 5, 1, 0, 0, 39, 40, 5, 4,
		0, 0, 40, 45, 5, 1, 0, 0, 41, 42, 5, 4, 0, 0, 42, 44, 5, 1, 0, 0, 43, 41,
		1, 0, 0, 0, 44, 47, 1, 0, 0, 0, 45, 43, 1, 0, 0, 0, 45, 46, 1, 0, 0, 0,
		46, 5, 1, 0, 0, 0, 47, 45, 1, 0, 0, 0, 48, 49, 5, 1, 0, 0, 49, 50, 5, 3,
		0, 0, 50, 55, 5, 1, 0, 0, 51, 52, 5, 3, 0, 0, 52, 54, 5, 1, 0, 0, 53, 51,
		1, 0, 0, 0, 54, 57, 1, 0, 0, 0, 55, 53, 1, 0, 0, 0, 55, 56, 1, 0, 0, 0,
		56, 7, 1, 0, 0, 0, 57, 55, 1, 0, 0, 0, 58, 61, 3, 12, 6, 0, 59, 60, 5,
		2, 0, 0, 60, 62, 3, 12, 6, 0, 61, 59, 1, 0, 0, 0, 62, 63, 1, 0, 0, 0, 63,
		61, 1, 0, 0, 0, 63, 64, 1, 0, 0, 0, 64, 9, 1, 0, 0, 0, 65, 66, 5, 1, 0,
		0, 66, 67, 5, 5, 0, 0, 67, 68, 5, 1, 0, 0, 68, 69, 5, 6, 0, 0, 69, 70,
		5, 1, 0, 0, 70, 11, 1, 0, 0, 0, 71, 78, 5, 1, 0, 0, 72, 78, 3, 16, 8, 0,
		73, 74, 5, 8, 0, 0, 74, 75, 3, 2, 1, 0, 75, 76, 5, 9, 0, 0, 76, 78, 1,
		0, 0, 0, 77, 71, 1, 0, 0, 0, 77, 72, 1, 0, 0, 0, 77, 73, 1, 0, 0, 0, 78,
		13, 1, 0, 0, 0, 79, 82, 5, 1, 0, 0, 80, 82, 3, 16, 8, 0, 81, 79, 1, 0,
		0, 0, 81, 80, 1, 0, 0, 0, 82, 15, 1, 0, 0, 0, 83, 84, 5, 12, 0, 0, 84,
		89, 3, 14, 7, 0, 85, 86, 5, 14, 0, 0, 86, 88, 3, 14, 7, 0, 87, 85, 1, 0,
		0, 0, 88, 91, 1, 0, 0, 0, 89, 87, 1, 0, 0, 0, 89, 90, 1, 0, 0, 0, 90, 92,
		1, 0, 0, 0, 91, 89, 1, 0, 0, 0, 92, 93, 5, 13, 0, 0, 93, 17, 1, 0, 0, 0,
		9, 22, 27, 36, 45, 55, 63, 77, 81, 89,
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
	TaskOrchestrationParserRULE_basicExpression       = 6
	TaskOrchestrationParserRULE_task                  = 7
	TaskOrchestrationParserRULE_joinGroup             = 8
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
	p.SetState(25)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for ok := true; ok; ok = ((int64(_la) & ^0x3f) == 0 && ((int64(1)<<_la)&4354) != 0) {
		{
			p.SetState(18)
			p.Expression()
		}
		p.SetState(22)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)

		for _la == TaskOrchestrationParserSEMICOLON || _la == TaskOrchestrationParserNEWLINE {
			{
				p.SetState(19)
				_la = p.GetTokenStream().LA(1)

				if !(_la == TaskOrchestrationParserSEMICOLON || _la == TaskOrchestrationParserNEWLINE) {
					p.GetErrorHandler().RecoverInline(p)
				} else {
					p.GetErrorHandler().ReportMatch(p)
					p.Consume()
				}
			}

			p.SetState(24)
			p.GetErrorHandler().Sync(p)
			if p.HasError() {
				goto errorExit
			}
			_la = p.GetTokenStream().LA(1)
		}

		p.SetState(27)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
	}
	{
		p.SetState(29)
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
	SequenceExpression() ISequenceExpressionContext
	OrExpression() IOrExpressionContext
	AndExpression() IAndExpressionContext
	ConditionalExpression() IConditionalExpressionContext
	BasicExpression() IBasicExpressionContext

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

func (s *ExpressionContext) SequenceExpression() ISequenceExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(ISequenceExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(ISequenceExpressionContext)
}

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

func (s *ExpressionContext) AndExpression() IAndExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IAndExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IAndExpressionContext)
}

func (s *ExpressionContext) ConditionalExpression() IConditionalExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IConditionalExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IConditionalExpressionContext)
}

func (s *ExpressionContext) BasicExpression() IBasicExpressionContext {
	var t antlr.RuleContext
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBasicExpressionContext); ok {
			t = ctx.(antlr.RuleContext)
			break
		}
	}

	if t == nil {
		return nil
	}

	return t.(IBasicExpressionContext)
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
	p.SetState(36)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetInterpreter().AdaptivePredict(p.BaseParser, p.GetTokenStream(), 2, p.GetParserRuleContext()) {
	case 1:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(31)
			p.SequenceExpression()
		}

	case 2:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(32)
			p.OrExpression()
		}

	case 3:
		p.EnterOuterAlt(localctx, 3)
		{
			p.SetState(33)
			p.AndExpression()
		}

	case 4:
		p.EnterOuterAlt(localctx, 4)
		{
			p.SetState(34)
			p.ConditionalExpression()
		}

	case 5:
		p.EnterOuterAlt(localctx, 5)
		{
			p.SetState(35)
			p.BasicExpression()
		}

	case antlr.ATNInvalidAltNumber:
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

// IOrExpressionContext is an interface to support dynamic dispatch.
type IOrExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	AllTASK_NAME() []antlr.TerminalNode
	TASK_NAME(i int) antlr.TerminalNode
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

func (s *OrExpressionContext) AllTASK_NAME() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserTASK_NAME)
}

func (s *OrExpressionContext) TASK_NAME(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserTASK_NAME, i)
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
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(38)
		p.Match(TaskOrchestrationParserTASK_NAME)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(39)
		p.Match(TaskOrchestrationParserOR)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(40)
		p.Match(TaskOrchestrationParserTASK_NAME)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(45)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == TaskOrchestrationParserOR {
		{
			p.SetState(41)
			p.Match(TaskOrchestrationParserOR)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(42)
			p.Match(TaskOrchestrationParserTASK_NAME)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

		p.SetState(47)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
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
	AllTASK_NAME() []antlr.TerminalNode
	TASK_NAME(i int) antlr.TerminalNode
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

func (s *AndExpressionContext) AllTASK_NAME() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserTASK_NAME)
}

func (s *AndExpressionContext) TASK_NAME(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserTASK_NAME, i)
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
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(48)
		p.Match(TaskOrchestrationParserTASK_NAME)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(49)
		p.Match(TaskOrchestrationParserAND)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(50)
		p.Match(TaskOrchestrationParserTASK_NAME)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	p.SetState(55)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == TaskOrchestrationParserAND {
		{
			p.SetState(51)
			p.Match(TaskOrchestrationParserAND)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(52)
			p.Match(TaskOrchestrationParserTASK_NAME)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

		p.SetState(57)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
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
	AllBasicExpression() []IBasicExpressionContext
	BasicExpression(i int) IBasicExpressionContext
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

func (s *SequenceExpressionContext) AllBasicExpression() []IBasicExpressionContext {
	children := s.GetChildren()
	len := 0
	for _, ctx := range children {
		if _, ok := ctx.(IBasicExpressionContext); ok {
			len++
		}
	}

	tst := make([]IBasicExpressionContext, len)
	i := 0
	for _, ctx := range children {
		if t, ok := ctx.(IBasicExpressionContext); ok {
			tst[i] = t.(IBasicExpressionContext)
			i++
		}
	}

	return tst
}

func (s *SequenceExpressionContext) BasicExpression(i int) IBasicExpressionContext {
	var t antlr.RuleContext
	j := 0
	for _, ctx := range s.GetChildren() {
		if _, ok := ctx.(IBasicExpressionContext); ok {
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

	return t.(IBasicExpressionContext)
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
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(58)
		p.BasicExpression()
	}
	p.SetState(61)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for ok := true; ok; ok = _la == TaskOrchestrationParserARROW {
		{
			p.SetState(59)
			p.Match(TaskOrchestrationParserARROW)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(60)
			p.BasicExpression()
		}

		p.SetState(63)
		p.GetErrorHandler().Sync(p)
		if p.HasError() {
			goto errorExit
		}
		_la = p.GetTokenStream().LA(1)
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
	AllTASK_NAME() []antlr.TerminalNode
	TASK_NAME(i int) antlr.TerminalNode
	QUESTION() antlr.TerminalNode
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

func (s *ConditionalExpressionContext) AllTASK_NAME() []antlr.TerminalNode {
	return s.GetTokens(TaskOrchestrationParserTASK_NAME)
}

func (s *ConditionalExpressionContext) TASK_NAME(i int) antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserTASK_NAME, i)
}

func (s *ConditionalExpressionContext) QUESTION() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserQUESTION, 0)
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
	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(65)
		p.Match(TaskOrchestrationParserTASK_NAME)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(66)
		p.Match(TaskOrchestrationParserQUESTION)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(67)
		p.Match(TaskOrchestrationParserTASK_NAME)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(68)
		p.Match(TaskOrchestrationParserCOLON)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(69)
		p.Match(TaskOrchestrationParserTASK_NAME)
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

// IBasicExpressionContext is an interface to support dynamic dispatch.
type IBasicExpressionContext interface {
	antlr.ParserRuleContext

	// GetParser returns the parser.
	GetParser() antlr.Parser

	// Getter signatures
	TASK_NAME() antlr.TerminalNode
	JoinGroup() IJoinGroupContext
	LPAREN() antlr.TerminalNode
	Expression() IExpressionContext
	RPAREN() antlr.TerminalNode

	// IsBasicExpressionContext differentiates from other interfaces.
	IsBasicExpressionContext()
}

type BasicExpressionContext struct {
	antlr.BaseParserRuleContext
	parser antlr.Parser
}

func NewEmptyBasicExpressionContext() *BasicExpressionContext {
	p := new(BasicExpressionContext)
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_basicExpression
	return p
}

func InitEmptyBasicExpressionContext(p *BasicExpressionContext) {
	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, nil, -1)
	p.RuleIndex = TaskOrchestrationParserRULE_basicExpression
}

func (*BasicExpressionContext) IsBasicExpressionContext() {}

func NewBasicExpressionContext(parser antlr.Parser, parent antlr.ParserRuleContext, invokingState int) *BasicExpressionContext {
	p := new(BasicExpressionContext)

	antlr.InitBaseParserRuleContext(&p.BaseParserRuleContext, parent, invokingState)

	p.parser = parser
	p.RuleIndex = TaskOrchestrationParserRULE_basicExpression

	return p
}

func (s *BasicExpressionContext) GetParser() antlr.Parser { return s.parser }

func (s *BasicExpressionContext) TASK_NAME() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserTASK_NAME, 0)
}

func (s *BasicExpressionContext) JoinGroup() IJoinGroupContext {
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

func (s *BasicExpressionContext) LPAREN() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserLPAREN, 0)
}

func (s *BasicExpressionContext) Expression() IExpressionContext {
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

func (s *BasicExpressionContext) RPAREN() antlr.TerminalNode {
	return s.GetToken(TaskOrchestrationParserRPAREN, 0)
}

func (s *BasicExpressionContext) GetRuleContext() antlr.RuleContext {
	return s
}

func (s *BasicExpressionContext) ToStringTree(ruleNames []string, recog antlr.Recognizer) string {
	return antlr.TreesStringTree(s, ruleNames, recog)
}

func (s *BasicExpressionContext) EnterRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.EnterBasicExpression(s)
	}
}

func (s *BasicExpressionContext) ExitRule(listener antlr.ParseTreeListener) {
	if listenerT, ok := listener.(TaskOrchestrationParserListener); ok {
		listenerT.ExitBasicExpression(s)
	}
}

func (s *BasicExpressionContext) Accept(visitor antlr.ParseTreeVisitor) interface{} {
	switch t := visitor.(type) {
	case TaskOrchestrationParserVisitor:
		return t.VisitBasicExpression(s)

	default:
		return t.VisitChildren(s)
	}
}

func (p *TaskOrchestrationParser) BasicExpression() (localctx IBasicExpressionContext) {
	localctx = NewBasicExpressionContext(p, p.GetParserRuleContext(), p.GetState())
	p.EnterRule(localctx, 12, TaskOrchestrationParserRULE_basicExpression)
	p.SetState(77)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case TaskOrchestrationParserTASK_NAME:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(71)
			p.Match(TaskOrchestrationParserTASK_NAME)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case TaskOrchestrationParserLBRACE:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(72)
			p.JoinGroup()
		}

	case TaskOrchestrationParserLPAREN:
		p.EnterOuterAlt(localctx, 3)
		{
			p.SetState(73)
			p.Match(TaskOrchestrationParserLPAREN)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}
		{
			p.SetState(74)
			p.Expression()
		}
		{
			p.SetState(75)
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
	p.EnterRule(localctx, 14, TaskOrchestrationParserRULE_task)
	p.SetState(81)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}

	switch p.GetTokenStream().LA(1) {
	case TaskOrchestrationParserTASK_NAME:
		p.EnterOuterAlt(localctx, 1)
		{
			p.SetState(79)
			p.Match(TaskOrchestrationParserTASK_NAME)
			if p.HasError() {
				// Recognition error - abort rule
				goto errorExit
			}
		}

	case TaskOrchestrationParserLBRACE:
		p.EnterOuterAlt(localctx, 2)
		{
			p.SetState(80)
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
	p.EnterRule(localctx, 16, TaskOrchestrationParserRULE_joinGroup)
	var _la int

	p.EnterOuterAlt(localctx, 1)
	{
		p.SetState(83)
		p.Match(TaskOrchestrationParserLBRACE)
		if p.HasError() {
			// Recognition error - abort rule
			goto errorExit
		}
	}
	{
		p.SetState(84)
		p.Task()
	}
	p.SetState(89)
	p.GetErrorHandler().Sync(p)
	if p.HasError() {
		goto errorExit
	}
	_la = p.GetTokenStream().LA(1)

	for _la == TaskOrchestrationParserCOMMA {
		{
			p.SetState(85)
			p.Match(TaskOrchestrationParserCOMMA)
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
	}
	{
		p.SetState(92)
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
