

parser grammar TaskOrchestrationParser;

options { tokenVocab=TaskOrchestrationLexer; }

// 语法规则
program
    : (expression (SEMICOLON | NEWLINE)*)+ EOF
    ;

// 表达式，按优先级从低到高排列
expression
    : orExpression
    ;

orExpression
    : andExpression (OR andExpression)*
    ;

andExpression
    : sequenceExpression (AND sequenceExpression)*
    ;

sequenceExpression
    : conditionalExpression (ARROW conditionalExpression)*
    ;

conditionalExpression
    : repetitionExpression (QUESTION expression COLON expression)?
    ;

repetitionExpression
    : primaryExpression (STAR)?
    ;

primaryExpression
    : task
    | LPAREN expression RPAREN
    ;

// 任务节点
task
    : TASK_NAME
    | parallelGroup
    | joinGroup
    ;

// 并行组: [A,B,C]
parallelGroup
    : LBRACKET task (COMMA task)* RBRACKET
    ;

// 汇聚组: {A,B,C}
joinGroup
    : LBRACE task (COMMA task)* RBRACE
    ;


