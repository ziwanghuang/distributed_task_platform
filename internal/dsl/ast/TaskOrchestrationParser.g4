

parser grammar TaskOrchestrationParser;

options { tokenVocab=TaskOrchestrationLexer; }

// 语法规则
program
    : (expression (SEMICOLON | NEWLINE)*)+ EOF
    ;

// 表达式，按优先级从低到高排列
expression
    : sequenceExpression
    | orExpression
    | andExpression
    | conditionalExpression
    | basicExpression
    ;

orExpression
    : TASK_NAME OR TASK_NAME (OR TASK_NAME)*
    ;

andExpression
    : TASK_NAME AND TASK_NAME (AND TASK_NAME)*
    ;

sequenceExpression
    : basicExpression (ARROW basicExpression)+
    ;

conditionalExpression
    : TASK_NAME QUESTION TASK_NAME COLON TASK_NAME
    ;

basicExpression
    : TASK_NAME
    | joinGroup
    | LPAREN expression RPAREN
    ;

// 任务节点
task
    : TASK_NAME
    | joinGroup
    ;



// 汇聚组: {A,B,C}
joinGroup
    : LBRACE task (COMMA task)* RBRACE
    ;


