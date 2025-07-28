# ANTLR4 任务编排DSL Makefile

# ANTLR4 工具路径 (macOS安装的ANTLR)
ANTLR4 = java -jar /opt/homebrew/Cellar/antlr/4.13.2/antlr-4.13.2-complete.jar

# 目标语言
LANGUAGE = Go

# 输出目录
OUTPUT_DIR = parser

# 语法文件
LEXER = TaskOrchestrationLexer.g4
PARSER = TaskOrchestrationParser.g4

# 生成的文件
GENERATED_FILES = $(OUTPUT_DIR)/*.go $(OUTPUT_DIR)/*.tokens $(OUTPUT_DIR)/*.interp

.PHONY: all clean generate test ast

# 默认目标
all: generate

# 生成ANTLR代码到parser目录
ast: $(LEXER) $(PARSER)
	@echo "生成ANTLR代码到ast目录..."
	@mkdir -p $(OUTPUT_DIR)
	$(ANTLR4) -Dlanguage=$(LANGUAGE) -visitor -listener -package parser -o $(OUTPUT_DIR) $(LEXER)
	$(ANTLR4) -Dlanguage=$(LANGUAGE) -visitor -listener -package parser -o $(OUTPUT_DIR) $(PARSER)
	@echo "代码生成完成到 $(OUTPUT_DIR)/ 目录"

# 生成ANTLR代码到当前目录
generate: $(LEXER) $(PARSER)
	@echo "生成ANTLR代码到当前目录..."
	$(ANTLR4) -Dlanguage=$(LANGUAGE) -visitor -listener -package dsl -o . $(LEXER)
	$(ANTLR4) -Dlanguage=$(LANGUAGE) -visitor -listener -package dsl -o . $(PARSER)
	@echo "代码生成完成"

# 清理生成的文件
clean:
	@echo "清理生成的文件..."
	rm -f $(GENERATED_FILES)
	@echo "清理完成"





# 安装ANTLR4 (macOS)
install-antlr4-mac:
	@echo "安装ANTLR4..."
	brew install antlr
	@echo "ANTLR4安装完成"

# 安装ANTLR4 (Ubuntu/Debian)
install-antlr4-ubuntu:
	@echo "安装ANTLR4..."
	sudo apt-get update
	sudo apt-get install -y antlr4
	@echo "ANTLR4安装完成"

# 显示帮助信息
help:
	@echo "可用的目标:"
	@echo "  all              - 生成所有代码到当前目录 (默认)"
	@echo "  ast              - 生成ANTLR代码到ast目录"
	@echo "  generate         - 生成ANTLR代码到当前目录"
	@echo "  install-antlr4-mac    - 在macOS上安装ANTLR4"
	@echo "  install-antlr4-ubuntu - 在Ubuntu/Debian上安装ANTLR4"
	@echo "  help             - 显示此帮助信息" 