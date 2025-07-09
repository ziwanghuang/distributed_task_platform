// Code generated from TaskOrchestrationParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package parser // TaskOrchestrationParser
import "github.com/antlr4-go/antlr/v4"

type BaseTaskOrchestrationParserVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseTaskOrchestrationParserVisitor) VisitProgram(ctx *ProgramContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitExpression(ctx *ExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitOrExpression(ctx *OrExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitAndExpression(ctx *AndExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitSequenceExpression(ctx *SequenceExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitConditionalExpression(ctx *ConditionalExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitRepetitionExpression(ctx *RepetitionExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitPrimaryExpression(ctx *PrimaryExpressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitTask(ctx *TaskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitParallelGroup(ctx *ParallelGroupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseTaskOrchestrationParserVisitor) VisitJoinGroup(ctx *JoinGroupContext) interface{} {
	return v.VisitChildren(ctx)
}
