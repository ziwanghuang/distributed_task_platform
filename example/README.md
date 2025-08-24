# 任务平台示例

## 启动任务平台
```
在项目的根目录下执行
make run_scheduler（启动调度器）
```

## 启动长任务
```
首先启动longrunning下的main.go（启动了executor，他是真正执行者。处理任务然后上报进度）,然后通过start_test.go添加长任务准备执行，等待三秒等scheduler调度start_test.go添加的任务
```

## 启动分片任务
```
首先启动shardig下的main.go（启动了executor，他是真正执行者。处理任务然后上报进度），然后通过start_test.go添加分片任务准备执行,等待三秒等scheduler调度start_test.go添加的任务
```