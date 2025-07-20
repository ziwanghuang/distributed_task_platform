// Code generated from TaskOrchestrationParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package parser // TaskOrchestrationParser
import "github.com/antlr4-go/antlr/v4"

// TaskOrchestrationParserListener is a complete listener for a parse tree produced by TaskOrchestrationParser.
type TaskOrchestrationParserListener interface {
	antlr.ParseTreeListener

	// EnterProgram is called when entering the program production.
	EnterProgram(c *ProgramContext)

	// EnterExpression is called when entering the expression production.
	EnterExpression(c *ExpressionContext)

	// EnterOrExpression is called when entering the orExpression production.
	EnterOrExpression(c *OrExpressionContext)

	// EnterAndExpression is called when entering the andExpression production.
	EnterAndExpression(c *AndExpressionContext)

	// EnterSequenceExpression is called when entering the sequenceExpression production.
	EnterSequenceExpression(c *SequenceExpressionContext)

	// EnterConditionalExpression is called when entering the conditionalExpression production.
	EnterConditionalExpression(c *ConditionalExpressionContext)

	// EnterBasicExpression is called when entering the basicExpression production.
	EnterBasicExpression(c *BasicExpressionContext)

	// EnterTask is called when entering the task production.
	EnterTask(c *TaskContext)

	// EnterJoinGroup is called when entering the joinGroup production.
	EnterJoinGroup(c *JoinGroupContext)

	// ExitProgram is called when exiting the program production.
	ExitProgram(c *ProgramContext)

	// ExitExpression is called when exiting the expression production.
	ExitExpression(c *ExpressionContext)

	// ExitOrExpression is called when exiting the orExpression production.
	ExitOrExpression(c *OrExpressionContext)

	// ExitAndExpression is called when exiting the andExpression production.
	ExitAndExpression(c *AndExpressionContext)

	// ExitSequenceExpression is called when exiting the sequenceExpression production.
	ExitSequenceExpression(c *SequenceExpressionContext)

	// ExitConditionalExpression is called when exiting the conditionalExpression production.
	ExitConditionalExpression(c *ConditionalExpressionContext)

	// ExitBasicExpression is called when exiting the basicExpression production.
	ExitBasicExpression(c *BasicExpressionContext)

	// ExitTask is called when exiting the task production.
	ExitTask(c *TaskContext)

	// ExitJoinGroup is called when exiting the joinGroup production.
	ExitJoinGroup(c *JoinGroupContext)
}
