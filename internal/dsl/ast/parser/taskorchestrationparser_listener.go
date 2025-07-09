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

	// EnterRepetitionExpression is called when entering the repetitionExpression production.
	EnterRepetitionExpression(c *RepetitionExpressionContext)

	// EnterPrimaryExpression is called when entering the primaryExpression production.
	EnterPrimaryExpression(c *PrimaryExpressionContext)

	// EnterTask is called when entering the task production.
	EnterTask(c *TaskContext)

	// EnterParallelGroup is called when entering the parallelGroup production.
	EnterParallelGroup(c *ParallelGroupContext)

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

	// ExitRepetitionExpression is called when exiting the repetitionExpression production.
	ExitRepetitionExpression(c *RepetitionExpressionContext)

	// ExitPrimaryExpression is called when exiting the primaryExpression production.
	ExitPrimaryExpression(c *PrimaryExpressionContext)

	// ExitTask is called when exiting the task production.
	ExitTask(c *TaskContext)

	// ExitParallelGroup is called when exiting the parallelGroup production.
	ExitParallelGroup(c *ParallelGroupContext)

	// ExitJoinGroup is called when exiting the joinGroup production.
	ExitJoinGroup(c *JoinGroupContext)
}
