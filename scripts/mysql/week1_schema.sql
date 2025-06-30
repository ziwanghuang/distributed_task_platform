-- 第一周分布式任务调度系统数据库表结构
-- 精简版，只包含核心功能所需字段
-- 任务表：存储任务元数据和调度配置
CREATE TABLE `task` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '任务ID',
  `name` VARCHAR(255) NOT NULL COMMENT '任务名称',
  `cron_expr` VARCHAR(100) NOT NULL COMMENT 'cron表达式',
  `next_time` DATETIME NOT NULL COMMENT '下次执行时间',
  `status` VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' COMMENT '任务状态: ACTIVE-可调度, PREEMPTED-已抢占, INACTIVE-停止执行',
  `version` BIGINT NOT NULL DEFAULT 0 COMMENT '版本号，用于乐观锁',
  `schedule_node` VARCHAR(100) DEFAULT NULL COMMENT '调度节点标识',
  `grpc_config` JSON DEFAULT NULL COMMENT 'gRPC配置',
  `http_config` JSON DEFAULT NULL COMMENT 'HTTP配置',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `utime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX `idx_next_time_status` (`next_time`, `status`),
  INDEX `idx_status_utime` (`status`, `utime`),
  INDEX `idx_schedule_node` (`schedule_node`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '任务调度表';
-- 执行记录表：存储任务执行的历史记录
CREATE TABLE `execution` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY COMMENT '执行ID',
  `task_id` BIGINT NOT NULL COMMENT '任务ID',
  `status` VARCHAR(20) NOT NULL DEFAULT 'PREPARE' COMMENT '执行状态: PREPARE-初始化, RUNNING-执行中, RETRYABLE_FAILED-可重试失败, FAILED-失败, SUCCESS-成功',
  `stime` DATETIME DEFAULT NULL COMMENT '开始时间',
  `etime` DATETIME DEFAULT NULL COMMENT '结束时间',
  `retry_cnt` INT NOT NULL DEFAULT 0 COMMENT '重试次数',
  `retry_config` JSON DEFAULT NULL COMMENT '重试配置快照',
  `next_retry_time` DATETIME DEFAULT NULL COMMENT '下次重试时间',
  `ctime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `utime` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  INDEX `idx_task_id_ctime` (`task_id`, `ctime`),
  INDEX `idx_status_next_retry_time` (`status`, `next_retry_time`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT = '任务执行记录表';
-- 创建外键约束
ALTER TABLE `execution`
ADD CONSTRAINT `fk_execution_task` FOREIGN KEY (`task_id`) REFERENCES `task` (`id`);
-- 插入示例任务
INSERT INTO `task` (
    `name`,
    `cron_expr`,
    `next_time`,
    `status`,
    `grpc_config`
  )
VALUES (
    'user_data_sync',
    '0 */30 * * * *',
    '2024-01-01 00:30:00',
    'ACTIVE',
    '{"service_name": "UserDataSyncService"}'
  ),
  (
    'email_notification',
    '0 0 9 * * *',
    '2024-01-01 09:00:00',
    'ACTIVE',
    NULL
  ),
  (
    'data_cleanup',
    '0 0 2 * * *',
    '2024-01-01 02:00:00',
    'ACTIVE',
    '{"service_name": "DataCleanupService"}'
  );