lexer grammar TaskOrchestrationLexer;

// 任务名称：英文字母 + 数字 + 下划线
TASK_NAME
    : [a-zA-Z_][a-zA-Z0-9_]*
    ;

// 操作符
ARROW: '->';
AND: '&&';
OR: '||';
QUESTION: '?';
COLON: ':';
STAR: '*';

// 括号
LPAREN: '(';
RPAREN: ')';
LBRACKET: '[';
RBRACKET: ']';
LBRACE: '{';
RBRACE: '}';
COMMA: ',';

// 空白字符
WS
    : [ \t\r\n]+ -> skip
    ;

// 注释
COMMENT
    : '//' ~[\r\n]* -> skip
    ;

SEMICOLON: ';';
NEWLINE: [\r\n]+;
