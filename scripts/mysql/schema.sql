-- 分布式任务调度系统数据库表结构
-- 设计原则：
-- 1. 支持分库分表扩展
-- 2. 包含版本控制字段支持乐观锁
-- 3. 预留扩展字段支持功能演进
-- 任务表：存储任务元数据和调度配置
CREATE TABLE `task` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '任务ID',
  `name` VARCHAR(255) NOT NULL COMMENT '任务名称',
  `cron_expr` VARCHAR(100) NOT NULL COMMENT 'cron表达式',
  `next_time` DATETIME NOT NULL COMMENT '下次执行时间',
  `status` TINYINT NOT NULL DEFAULT 0 COMMENT '任务状态: 0-ACTIVE, 1-PREEMPTED, 2-INACTIVE',
  `version` BIGINT NOT NULL DEFAULT 0 COMMENT '版本号，用于乐观锁',
  `schedule_node` VARCHAR(100) DEFAULT NULL COMMENT '调度节点标识',
  `grpc_config` JSON DEFAULT NULL COMMENT 'gRPC配置',
  `http_config` JSON DEFAULT NULL COMMENT 'HTTP配置',
  `retry_config` JSON DEFAULT NULL COMMENT '重试配置',
  `shard_config` JSON DEFAULT NULL COMMENT '分片配置',
  `params` JSON DEFAULT NULL COMMENT '任务参数',
  `description` TEXT COMMENT '任务描述',
  `priority` INT DEFAULT 0 COMMENT '任务优先级',
  `timeout` INT DEFAULT 3600 COMMENT '任务超时时间(秒)',
  `max_retry` INT DEFAULT 3 COMMENT '最大重试次数',
  `creator` VARCHAR(100) DEFAULT NULL COMMENT '创建者',
  `updater` VARCHAR(100) DEFAULT NULL COMMENT '更新者',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `utime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX `idx_next_time_status` (`next_time`, `status`),
  INDEX `idx_status_utime` (`status`, `utime`),
  INDEX `idx_schedule_node` (`schedule_node`),
  INDEX `idx_name` (`name`),
  INDEX `idx_ctime` (`ctime`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '任务调度表';
-- 执行记录表：存储任务执行的历史记录
CREATE TABLE `execution` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '执行ID',
  `task_id` BIGINT NOT NULL COMMENT '任务ID',
  `task_name` VARCHAR(255) NOT NULL COMMENT '任务名称快照',
  `status` TINYINT NOT NULL DEFAULT 0 COMMENT '执行状态: 0-PREPARE, 1-RUNNING, 2-RETRYABLE_FAILED, 3-FAILED, 4-SUCCESS, 5-FAILED_PREEMPTED',
  `stime` DATETIME DEFAULT NULL COMMENT '开始时间',
  `etime` DATETIME DEFAULT NULL COMMENT '结束时间',
  `retry_cnt` INT DEFAULT 0 COMMENT '重试次数',
  `retry_config` JSON DEFAULT NULL COMMENT '重试配置快照',
  `next_retry_time` DATETIME DEFAULT NULL COMMENT '下次重试时间',
  `progress` INT DEFAULT 0 COMMENT '执行进度 0-100',
  `executor_node` VARCHAR(100) DEFAULT NULL COMMENT '执行节点标识',
  `params` JSON DEFAULT NULL COMMENT '执行参数',
  `result` TEXT COMMENT '执行结果',
  `error_msg` TEXT COMMENT '错误信息',
  `duration` INT DEFAULT 0 COMMENT '执行耗时(毫秒)',
  `shard_id` INT DEFAULT NULL COMMENT '分片ID',
  `shard_total` INT DEFAULT NULL COMMENT '分片总数',
  `parent_execution_id` BIGINT DEFAULT NULL COMMENT '父执行ID（用于分片）',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `utime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX `idx_task_id_ctime` (`task_id`, `ctime`),
  INDEX `idx_status_next_retry_time` (`status`, `next_retry_time`),
  INDEX `idx_executor_node` (`executor_node`),
  INDEX `idx_parent_execution_id` (`parent_execution_id`),
  INDEX `idx_stime` (`stime`),
  INDEX `idx_ctime` (`ctime`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '任务执行记录表';
-- 任务依赖关系表：存储任务间的依赖关系
CREATE TABLE `task_dependency` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '依赖ID',
  `task_id` BIGINT NOT NULL COMMENT '任务ID',
  `depend_task_id` BIGINT NOT NULL COMMENT '依赖的任务ID',
  `depend_type` VARCHAR(50) NOT NULL COMMENT '依赖类型: BEFORE, AFTER, PARALLEL',
  `condition_expr` JSON DEFAULT NULL COMMENT '条件表达式',
  `priority` INT DEFAULT 0 COMMENT '依赖优先级',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  UNIQUE KEY `uk_task_depend` (`task_id`, `depend_task_id`),
  INDEX `idx_depend_task_id` (`depend_task_id`),
  INDEX `idx_depend_type` (`depend_type`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '任务依赖关系表';
-- 节点负载信息表：存储执行节点的负载情况
CREATE TABLE `node_load` (
  `node_id` VARCHAR(100) PRIMARY KEY COMMENT '节点ID',
  `node_type` VARCHAR(50) NOT NULL COMMENT '节点类型: SCHEDULER, EXECUTOR',
  `cpu_usage` DECIMAL(5, 2) DEFAULT 0.00 COMMENT 'CPU使用率',
  `memory_usage` DECIMAL(5, 2) DEFAULT 0.00 COMMENT '内存使用率',
  `disk_usage` DECIMAL(5, 2) DEFAULT 0.00 COMMENT '磁盘使用率',
  `task_count` INT DEFAULT 0 COMMENT '当前任务数量',
  `max_task_count` INT DEFAULT 100 COMMENT '最大任务数量',
  `last_heartbeat` DATETIME NOT NULL COMMENT '最后心跳时间',
  `status` TINYINT NOT NULL DEFAULT 1 COMMENT '节点状态: 0-OFFLINE, 1-ONLINE, 2-BUSY',
  `metadata` JSON DEFAULT NULL COMMENT '节点元数据',
  `region` VARCHAR(50) DEFAULT NULL COMMENT '节点所在区域',
  `zone` VARCHAR(50) DEFAULT NULL COMMENT '节点所在可用区',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `utime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX `idx_node_type_status` (`node_type`, `status`),
  INDEX `idx_last_heartbeat` (`last_heartbeat`),
  INDEX `idx_region_zone` (`region`, `zone`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '节点负载信息表';
-- 执行日志表：存储任务执行的详细日志
CREATE TABLE `execution_log` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '日志ID',
  `execution_id` BIGINT NOT NULL COMMENT '执行ID',
  `task_id` BIGINT NOT NULL COMMENT '任务ID',
  `log_level` VARCHAR(20) NOT NULL COMMENT '日志级别: DEBUG, INFO, WARN, ERROR',
  `message` TEXT NOT NULL COMMENT '日志消息',
  `context` JSON DEFAULT NULL COMMENT '日志上下文',
  `trace_id` VARCHAR(100) DEFAULT NULL COMMENT '链路追踪ID',
  `span_id` VARCHAR(100) DEFAULT NULL COMMENT 'Span ID',
  `node_id` VARCHAR(100) DEFAULT NULL COMMENT '节点ID',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  INDEX `idx_execution_id_ctime` (`execution_id`, `ctime`),
  INDEX `idx_task_id_ctime` (`task_id`, `ctime`),
  INDEX `idx_log_level` (`log_level`),
  INDEX `idx_trace_id` (`trace_id`),
  INDEX `idx_ctime` (`ctime`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '执行日志表';
-- 调度历史表：记录调度决策历史
CREATE TABLE `schedule_history` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '历史ID',
  `task_id` BIGINT NOT NULL COMMENT '任务ID',
  `execution_id` BIGINT NOT NULL COMMENT '执行ID',
  `schedule_node` VARCHAR(100) NOT NULL COMMENT '调度节点',
  `executor_node` VARCHAR(100) DEFAULT NULL COMMENT '执行节点',
  `schedule_time` DATETIME NOT NULL COMMENT '调度时间',
  `preempt_time` DATETIME DEFAULT NULL COMMENT '抢占时间',
  `start_time` DATETIME DEFAULT NULL COMMENT '开始执行时间',
  `end_time` DATETIME DEFAULT NULL COMMENT '结束时间',
  `status` TINYINT NOT NULL COMMENT '调度状态',
  `load_factor` DECIMAL(5, 2) DEFAULT NULL COMMENT '负载因子',
  `strategy` VARCHAR(50) DEFAULT NULL COMMENT '调度策略',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  INDEX `idx_task_id_schedule_time` (`task_id`, `schedule_time`),
  INDEX `idx_schedule_node` (`schedule_node`),
  INDEX `idx_executor_node` (`executor_node`),
  INDEX `idx_schedule_time` (`schedule_time`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '调度历史表';
-- 规则配置表：存储DSL规则和编排配置
CREATE TABLE `rule_config` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '规则ID',
  `name` VARCHAR(255) NOT NULL COMMENT '规则名称',
  `type` VARCHAR(50) NOT NULL COMMENT '规则类型: SCHEDULE, ORCHESTRATION, RETRY',
  `dsl_content` TEXT NOT NULL COMMENT 'DSL规则内容',
  `compiled_rule` JSON DEFAULT NULL COMMENT '编译后的规则',
  `status` TINYINT NOT NULL DEFAULT 1 COMMENT '规则状态: 0-DISABLED, 1-ENABLED',
  `priority` INT DEFAULT 0 COMMENT '规则优先级',
  `description` TEXT COMMENT '规则描述',
  `creator` VARCHAR(100) DEFAULT NULL COMMENT '创建者',
  `updater` VARCHAR(100) DEFAULT NULL COMMENT '更新者',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `utime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  UNIQUE KEY `uk_name` (`name`),
  INDEX `idx_type_status` (`type`, `status`),
  INDEX `idx_priority` (`priority`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '规则配置表';
-- 分片执行记录表：记录任务分片的执行情况
CREATE TABLE `shard_execution` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '分片执行ID',
  `parent_execution_id` BIGINT NOT NULL COMMENT '父执行ID',
  `task_id` BIGINT NOT NULL COMMENT '任务ID',
  `shard_id` INT NOT NULL COMMENT '分片ID',
  `shard_key` VARCHAR(255) DEFAULT NULL COMMENT '分片键',
  `shard_range_start` BIGINT DEFAULT NULL COMMENT '分片范围开始',
  `shard_range_end` BIGINT DEFAULT NULL COMMENT '分片范围结束',
  `status` TINYINT NOT NULL DEFAULT 0 COMMENT '分片状态',
  `executor_node` VARCHAR(100) DEFAULT NULL COMMENT '执行节点',
  `progress` INT DEFAULT 0 COMMENT '执行进度',
  `result` TEXT COMMENT '执行结果',
  `error_msg` TEXT COMMENT '错误信息',
  `start_time` DATETIME DEFAULT NULL COMMENT '开始时间',
  `end_time` DATETIME DEFAULT NULL COMMENT '结束时间',
  `duration` INT DEFAULT 0 COMMENT '执行耗时(毫秒)',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `utime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX `idx_parent_execution_id` (`parent_execution_id`),
  INDEX `idx_task_id_shard_id` (`task_id`, `shard_id`),
  INDEX `idx_executor_node` (`executor_node`),
  INDEX `idx_status` (`status`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '分片执行记录表';
-- 创建外键约束
ALTER TABLE `execution`
ADD CONSTRAINT `fk_execution_task` FOREIGN KEY (`task_id`) REFERENCES `task` (`id`);
ALTER TABLE `task_dependency`
ADD CONSTRAINT `fk_dependency_task` FOREIGN KEY (`task_id`) REFERENCES `task` (`id`);
ALTER TABLE `task_dependency`
ADD CONSTRAINT `fk_dependency_depend_task` FOREIGN KEY (`depend_task_id`) REFERENCES `task` (`id`);
ALTER TABLE `execution_log`
ADD CONSTRAINT `fk_log_execution` FOREIGN KEY (`execution_id`) REFERENCES `execution` (`id`);
ALTER TABLE `schedule_history`
ADD CONSTRAINT `fk_history_task` FOREIGN KEY (`task_id`) REFERENCES `task` (`id`);
ALTER TABLE `schedule_history`
ADD CONSTRAINT `fk_history_execution` FOREIGN KEY (`execution_id`) REFERENCES `execution` (`id`);
ALTER TABLE `shard_execution`
ADD CONSTRAINT `fk_shard_execution` FOREIGN KEY (`parent_execution_id`) REFERENCES `execution` (`id`);
ALTER TABLE `shard_execution`
ADD CONSTRAINT `fk_shard_task` FOREIGN KEY (`task_id`) REFERENCES `task` (`id`);
-- 初始化数据
-- 插入示例任务
INSERT INTO `task` (
    `name`,
    `cron_expr`,
    `next_time`,
    `status`,
    `retry_config`,
    `description`,
    `priority`
  )
VALUES (
    'user_data_sync',
    '0 */30 * * * *',
    '2024-01-01 00:30:00',
    0,
    '{"max_retries": 3, "initial_interval": "30s", "max_interval": "300s", "multiplier": 2.0}',
    '用户数据同步任务',
    100
  ),
  (
    'email_notification',
    '0 0 9 * * *',
    '2024-01-01 09:00:00',
    0,
    '{"max_retries": 2, "initial_interval": "60s", "max_interval": "600s", "multiplier": 1.5}',
    '邮件通知任务',
    50
  ),
  (
    'data_cleanup',
    '0 0 2 * * *',
    '2024-01-01 02:00:00',
    0,
    '{"max_retries": 1, "initial_interval": "120s", "max_interval": "1200s", "multiplier": 3.0}',
    '数据清理任务',
    30
  );
-- 插入示例规则
INSERT INTO `rule_config` (
    `name`,
    `type`,
    `dsl_content`,
    `status`,
    `priority`,
    `description`
  )
VALUES (
    'high_priority_first',
    'SCHEDULE',
    'RULE high_priority_first { WHEN task.priority > 80 THEN schedule_immediately() }',
    1,
    100,
    '高优先级任务优先调度'
  ),
  (
    'retry_on_failure',
    'RETRY',
    'RULE retry_on_failure { WHEN execution.status == FAILED AND execution.retry_count < task.max_retry THEN retry_with_backoff() }',
    1,
    90,
    '失败任务重试规则'
  );