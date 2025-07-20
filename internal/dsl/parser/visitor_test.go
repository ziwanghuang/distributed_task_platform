//go:build unit

package parser

import (
	"testing"

	"gitee.com/flycash/distributed_task_platform/internal/dsl/ast/parser"
	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskOrchestrationVisitor_Visit(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result any)
	}{
		{
			name:  "simple sequence",
			input: "A->B->C;",
			check: func(t *testing.T, result any) {
				planRes, ok := result.(*planRes)
				require.True(t, ok)
				assert.NoError(t, planRes.err)
				assert.Len(t, planRes.root, 1)
				assert.Equal(t, "A", planRes.root[0].Node.ChildNodes()[0])
			},
		},
		{
			name:  "complex workflow with parallel and join",
			input: "A->B->(C&&D); C->(E||F)->end; D->E->end;",
			check: func(t *testing.T, result any) {
				planRes, ok := result.(*planRes)
				require.True(t, ok)
				assert.NoError(t, planRes.err)
				assert.Len(t, planRes.root, 1)

				// 检查根节点是A
				rootNode := planRes.root[0]
				assert.Equal(t, "A", rootNode.Node.ChildNodes()[0])

				// 检查任务映射包含所有任务
				taskNames := []string{"A", "B", "C", "D", "E", "F", "end"}
				for _, name := range taskNames {
					task, exists := planRes.tasks.Load(name)
					assert.True(t, exists, "Task %s should exist", name)
					assert.NotNil(t, task)
				}

				// 检查结束节点
				assert.Equal(t, "end", planRes.end.Node.ChildNodes()[0])

				// 详细验证每个任务的pre和next关系
				aTask, _ := planRes.tasks.Load("A")
				bTask, _ := planRes.tasks.Load("B")
				cTask, _ := planRes.tasks.Load("C")
				dTask, _ := planRes.tasks.Load("D")
				eTask, _ := planRes.tasks.Load("E")
				fTask, _ := planRes.tasks.Load("F")
				endTask, _ := planRes.tasks.Load("end")

				// 验证A任务的关系
				// A是根节点，没有pre任务
				assert.Nil(t, aTask.Pre, "Task A should have no pre task")
				// A的next应该是B
				assert.Equal(t, "B", aTask.Next.ChildNodes()[0], "Task A's next should be B")
				assert.Equal(t, NodeTypeSingle, aTask.Next.Type(), "Task A's next should be SingleNode")

				// 验证B任务的关系
				// B的pre应该是A
				assert.Equal(t, "A", bTask.Pre.ChildNodes()[0], "Task B's pre should be A")
				assert.Equal(t, NodeTypeSingle, bTask.Pre.Type(), "Task B's pre should be SingleNode")
				// B的next应该是AndNode，包含C和D
				bNextNames := bTask.Next.ChildNodes()
				assert.Contains(t, bNextNames, "C", "Task B's next should contain C")
				assert.Contains(t, bNextNames, "D", "Task B's next should contain D")
				assert.Equal(t, NodeTypeAnd, bTask.Next.Type(), "Task B's next should be AndNode")

				// 验证C任务的关系
				// C的pre应该包含B（通过AndNode）
				cPreNames := cTask.Pre.ChildNodes()
				assert.Contains(t, cPreNames, "B", "Task C's pre should contain B")
				assert.Equal(t, NodeTypeSingle, cTask.Pre.Type(), "Task C's pre should be AndNode")
				// C的next应该是OrNode，包含E和F
				cNextNames := cTask.Next.ChildNodes()
				assert.Contains(t, cNextNames, "E", "Task C's next should contain E")
				assert.Contains(t, cNextNames, "F", "Task C's next should contain F")
				assert.Equal(t, NodeTypeOr, cTask.Next.Type(), "Task C's next should be OrNode")

				// 验证D任务的关系
				// D的pre应该包含B（通过AndNode）
				dPreNames := dTask.Pre.ChildNodes()
				assert.Contains(t, dPreNames, "B", "Task D's pre should contain B")
				assert.Equal(t, NodeTypeSingle, dTask.Pre.Type(), "Task D's pre should be AndNode")
				// D的next应该是E
				assert.Equal(t, "E", dTask.Next.ChildNodes()[0], "Task D's next should be E")
				assert.Equal(t, NodeTypeSingle, dTask.Next.Type(), "Task D's next should be SingleNode")

				// 验证E任务的关系
				// E的pre应该包含C和D
				ePreNames := eTask.Pre.ChildNodes()
				assert.Contains(t, ePreNames, "C", "Task E's pre should contain C")
				assert.Contains(t, ePreNames, "D", "Task E's pre should contain D")
				assert.Equal(t, NodeTypeAnd, eTask.Pre.Type(), "Task E's pre should be OrNode")
				// E的next应该是end
				assert.Equal(t, "end", eTask.Next.ChildNodes()[0], "Task E's next should be end")
				assert.Equal(t, NodeTypeEnd, eTask.Next.Type(), "Task E's next should be EndNode")

				// 验证F任务的关系
				// F的pre应该包含C（通过OrNode）
				fPreNames := fTask.Pre.ChildNodes()
				assert.Contains(t, fPreNames, "C", "Task F's pre should contain C")
				assert.Equal(t, NodeTypeSingle, fTask.Pre.Type(), "Task F's pre should be OrNode")
				// F的next应该是end
				assert.Equal(t, "end", fTask.Next.ChildNodes()[0], "Task F's next should be end")
				assert.Equal(t, NodeTypeEnd, fTask.Next.Type(), "Task F's next should be EndNode")

				// 验证end任务的关系
				// end的pre应该包含E和F
				endPreNames := endTask.Pre.ChildNodes()
				assert.Contains(t, endPreNames, "E", "Task end's pre should contain E")
				assert.Contains(t, endPreNames, "F", "Task end's pre should contain F")
				assert.Equal(t, NodeTypeOr, endTask.Pre.Type(), "Task end's pre should be OrNode")
				// end没有next任务
				assert.Nil(t, endTask.Next, "Task end should have no next task")
			},
		},

		{
			name:  "join groups",
			input: "{A,B,C}->D;",
			check: func(t *testing.T, result any) {
				planRes, ok := result.(*planRes)
				require.True(t, ok)
				assert.NoError(t, planRes.err)

				// 检查D的pre应该是OrNode
				dTask, _ := planRes.tasks.Load("D")
				assert.Equal(t, NodeTypeOr, dTask.Pre.Type())
				preNames := dTask.Pre.ChildNodes()
				assert.Contains(t, preNames, "A")
				assert.Contains(t, preNames, "B")
				assert.Contains(t, preNames, "C")
			},
		},
		{
			name:  "conditional expression",
			input: "(A?B:C)->end;",
			check: func(t *testing.T, result any) {
				planRes, ok := result.(*planRes)
				require.True(t, ok)
				assert.NoError(t, planRes.err)

				// 检查A节点的类型应该是ConditionNode
				aTask, exists := planRes.tasks.Load("A")
				assert.True(t, exists)
				assert.Equal(t, NodeTypeCondition, aTask.Next.Type())

				endTask, exists := planRes.tasks.Load("end")
				assert.True(t, exists)
				assert.Equal(t, NodeTypeEnd, endTask.Node.Type())
				assert.Equal(t, NodeTypeCondition, endTask.Pre.Type())
				assert.Equal(t, []string{"B"}, endTask.Pre.NextNodes(Execution{
					Status: TaskExecutionStatusSuccess,
				}))
				assert.Equal(t, []string{"C"}, endTask.Pre.NextNodes(Execution{
					Status: TaskExecutionStatusFailed,
				}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建词法分析器
			lexer := parser.NewTaskOrchestrationLexer(antlr.NewInputStream(tt.input))
			tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)

			// 创建语法分析器
			p := parser.NewTaskOrchestrationParser(tokens)
			programCtx := p.Program()

			// 创建访问者并执行Visit
			visitor := NewTaskOrchestrationVisitor()
			result := visitor.Visit(programCtx)

			// 执行检查
			tt.check(t, result)
		})
	}
}
