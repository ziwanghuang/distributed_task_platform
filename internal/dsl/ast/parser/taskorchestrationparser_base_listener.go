// Code generated from TaskOrchestrationParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package parser // TaskOrchestrationParser
import "github.com/antlr4-go/antlr/v4"

// BaseTaskOrchestrationParserListener is a complete listener for a parse tree produced by TaskOrchestrationParser.
type BaseTaskOrchestrationParserListener struct{}

var _ TaskOrchestrationParserListener = &BaseTaskOrchestrationParserListener{}

// VisitTerminal is called when a terminal node is visited.
func (s *BaseTaskOrchestrationParserListener) VisitTerminal(node antlr.TerminalNode) {}

// VisitErrorNode is called when an error node is visited.
func (s *BaseTaskOrchestrationParserListener) VisitErrorNode(node antlr.ErrorNode) {}

// EnterEveryRule is called when any rule is entered.
func (s *BaseTaskOrchestrationParserListener) EnterEveryRule(ctx antlr.ParserRuleContext) {}

// ExitEveryRule is called when any rule is exited.
func (s *BaseTaskOrchestrationParserListener) ExitEveryRule(ctx antlr.ParserRuleContext) {}

// EnterProgram is called when production program is entered.
func (s *BaseTaskOrchestrationParserListener) EnterProgram(ctx *ProgramContext) {}

// ExitProgram is called when production program is exited.
func (s *BaseTaskOrchestrationParserListener) ExitProgram(ctx *ProgramContext) {}

// EnterExpression is called when production expression is entered.
func (s *BaseTaskOrchestrationParserListener) EnterExpression(ctx *ExpressionContext) {}

// ExitExpression is called when production expression is exited.
func (s *BaseTaskOrchestrationParserListener) ExitExpression(ctx *ExpressionContext) {}

// EnterOrExpression is called when production orExpression is entered.
func (s *BaseTaskOrchestrationParserListener) EnterOrExpression(ctx *OrExpressionContext) {}

// ExitOrExpression is called when production orExpression is exited.
func (s *BaseTaskOrchestrationParserListener) ExitOrExpression(ctx *OrExpressionContext) {}

// EnterAndExpression is called when production andExpression is entered.
func (s *BaseTaskOrchestrationParserListener) EnterAndExpression(ctx *AndExpressionContext) {}

// ExitAndExpression is called when production andExpression is exited.
func (s *BaseTaskOrchestrationParserListener) ExitAndExpression(ctx *AndExpressionContext) {}

// EnterSequenceExpression is called when production sequenceExpression is entered.
func (s *BaseTaskOrchestrationParserListener) EnterSequenceExpression(ctx *SequenceExpressionContext) {
}

// ExitSequenceExpression is called when production sequenceExpression is exited.
func (s *BaseTaskOrchestrationParserListener) ExitSequenceExpression(ctx *SequenceExpressionContext) {
}

// EnterConditionalExpression is called when production conditionalExpression is entered.
func (s *BaseTaskOrchestrationParserListener) EnterConditionalExpression(ctx *ConditionalExpressionContext) {
}

// ExitConditionalExpression is called when production conditionalExpression is exited.
func (s *BaseTaskOrchestrationParserListener) ExitConditionalExpression(ctx *ConditionalExpressionContext) {
}

// EnterRepetitionExpression is called when production repetitionExpression is entered.
func (s *BaseTaskOrchestrationParserListener) EnterRepetitionExpression(ctx *RepetitionExpressionContext) {
}

// ExitRepetitionExpression is called when production repetitionExpression is exited.
func (s *BaseTaskOrchestrationParserListener) ExitRepetitionExpression(ctx *RepetitionExpressionContext) {
}

// EnterPrimaryExpression is called when production primaryExpression is entered.
func (s *BaseTaskOrchestrationParserListener) EnterPrimaryExpression(ctx *PrimaryExpressionContext) {}

// ExitPrimaryExpression is called when production primaryExpression is exited.
func (s *BaseTaskOrchestrationParserListener) ExitPrimaryExpression(ctx *PrimaryExpressionContext) {}

// EnterTask is called when production task is entered.
func (s *BaseTaskOrchestrationParserListener) EnterTask(ctx *TaskContext) {}

// ExitTask is called when production task is exited.
func (s *BaseTaskOrchestrationParserListener) ExitTask(ctx *TaskContext) {}

// EnterParallelGroup is called when production parallelGroup is entered.
func (s *BaseTaskOrchestrationParserListener) EnterParallelGroup(ctx *ParallelGroupContext) {}

// ExitParallelGroup is called when production parallelGroup is exited.
func (s *BaseTaskOrchestrationParserListener) ExitParallelGroup(ctx *ParallelGroupContext) {}

// EnterJoinGroup is called when production joinGroup is entered.
func (s *BaseTaskOrchestrationParserListener) EnterJoinGroup(ctx *JoinGroupContext) {}

// ExitJoinGroup is called when production joinGroup is exited.
func (s *BaseTaskOrchestrationParserListener) ExitJoinGroup(ctx *JoinGroupContext) {}
