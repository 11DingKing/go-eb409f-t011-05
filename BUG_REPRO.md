# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

帮我查一个演练被静默判成完成的问题，先不要改代码，我要先拿到根因和证据。

现象：

1. 有些黑启动预案只登记了主路径，没有备用路径（现场还没评审出备用方案）。用这种预案跑演练，执行到一半上报主电源恢复（同期抢合闸），接口返回 200，演练状态直接切成 fallback、path 变成 backup、current_step 归零。
2. 紧接着再调一次执行下一步，接口返回 done=true，演练被判定成 completed。也就是说主路径剩下的步骤一步都没执行，整个黑启动流程被静默跳过。
3. 演练被判完成之后，舱区作业锁和离网窗口都被释放、改期的巡检工单也恢复了，事后翻演练台账看起来就是一次「成功完成的演练」，完全看不出跳步。
4. 登记了备用路径的预案行为是正常的：主电源恢复后切到备用路径，再逐步执行备用路径直到完成。
5. 演练前面的申请、批准、开始、逐步执行都正常，问题只出现在上报主电源恢复这一步。

复现：登记一个只有主路径、没有备用路径的黑启动预案，申请并批准演练，开始并执行一步，然后上报主电源恢复，看接口返回和演练状态；再调一次执行下一步。

请定位根因：说明是哪个 Go 文件里的哪个符号、它的什么错误行为，以及这个错误行为为什么会让演练被静默判成完成（包括为什么有备用路径的预案不受影响、为什么演练的锁和窗口也跟着被释放）。先给结论和证据，不要改仓库里的代码。

## 含 Bug 版本

- 仓库：11DingKing/go-eb409f-t011-05
- 仓库地址：https://github.com/11DingKing/go-eb409f-t011-05.git
- parent SHA：39a41970b26c952b926a4bf22a21dc2dc93ab5f6

## 复现步骤

```bash
git clone -- https://github.com/11DingKing/go-eb409f-t011-05.git bug-repro
cd bug-repro
git checkout --detach 39a41970b26c952b926a4bf22a21dc2dc93ab5f6
go test -timeout=120s ./internal/httpsrv/ -run "TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath|TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute|TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test -timeout=120s ./internal/httpsrv/ -run "TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath|TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute|TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne" -count=1 -v
=== RUN   TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath
    power_recovery_test.go:82: power recovery on a plan without a backup route: code = 200 want 409 (body map[cabin_id:cabin-1 completed_at:0001-01-01T00:00:00Z current_step:0 fallback_reason:rush close id:drill-1 path:backup plan_id:plan-no-backup restart_reason: started_at:2026-08-17T01:17:06.787381628Z status:fallback supervisor_id:sup team_id:crew-A])
--- FAIL: TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath (0.02s)
=== RUN   TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute
    power_recovery_test.go:113: power recovery: code = 200 want 409
--- FAIL: TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute (0.00s)
=== RUN   TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne
--- PASS: TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne (0.00s)
FAIL
FAIL	microgrid-dispatch/internal/httpsrv	0.083s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test -timeout=120s ./internal/httpsrv/ -run "TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath|TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute|TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne" -count=1 -v
=== RUN   TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath
    power_recovery_test.go:82: power recovery on a plan without a backup route: code = 200 want 409 (body map[cabin_id:cabin-1 completed_at:0001-01-01T00:00:00Z current_step:0 fallback_reason:rush close id:drill-1 path:backup plan_id:plan-no-backup restart_reason: started_at:2026-08-17T01:17:44.064834965Z status:fallback supervisor_id:sup team_id:crew-A])
--- FAIL: TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath (0.00s)
=== RUN   TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute
    power_recovery_test.go:113: power recovery: code = 200 want 409
--- FAIL: TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute (0.00s)
=== RUN   TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne
--- PASS: TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne (0.00s)
FAIL
FAIL	microgrid-dispatch/internal/httpsrv	0.006s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

通过标准（diagnosis）：
1. 命中 gold 根因涉及的文件：internal/domain/blackstart/drill.go
2. 命中 gold 根因涉及的符号：(*Drill).TriggerFallback
3. 命中正确的失效机制：该方法缺少「预案备用路径为空」的前置校验，对空备用路径（零值 nil 切片）照样把演练切到 backup 路径并把步骤游标归零；随后取当前步骤时按空路径判定为已耗尽，应用层据此直接完成演练并释放锁与离网窗口，主路径剩余步骤被整段跳过。还需说明该校验存在时应返回「预案无备用路径」哨兵并映射为 409，以及为什么有备用路径的预案不受影响
4. 结论有实际证据（读过相关代码或跑过复现），不是凭空推断
5. 目标仓库全程零改动；容器内一次性独立复现程序不计为项目代码改动
6. 复现依据：
   go test -timeout=120s ./internal/httpsrv/ -run 'TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath|TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute|TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne' -count=1 -v
   在 main 上失败、在 gold_model_fix 上通过
