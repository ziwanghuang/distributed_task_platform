// Code generated from TaskOrchestrationParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package parser // TaskOrchestrationParser
import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by TaskOrchestrationParser.
type TaskOrchestrationParserVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by TaskOrchestrationParser#program.
	VisitProgram(ctx *ProgramContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#expression.
	VisitExpression(ctx *ExpressionContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#orExpression.
	VisitOrExpression(ctx *OrExpressionContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#andExpression.
	VisitAndExpression(ctx *AndExpressionContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#sequenceExpression.
	VisitSequenceExpression(ctx *SequenceExpressionContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#conditionalExpression.
	VisitConditionalExpression(ctx *ConditionalExpressionContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#basicExpression.
	VisitBasicExpression(ctx *BasicExpressionContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#task.
	VisitTask(ctx *TaskContext) interface{}

	// Visit a parse tree produced by TaskOrchestrationParser#joinGroup.
	VisitJoinGroup(ctx *JoinGroupContext) interface{}
}
