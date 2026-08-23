# Release preparation and recovery

## 以当前仓库为准

发布表面会演进。先读取当前版本声明、`CHANGELOG.md`、构建配置、SDK manifests、
`.github/workflows/release.yml` 和相关测试，再决定需要修改和验证的文件。使用精准搜索
核对旧版本与目标版本，但不要把搜索命中数当作固定契约。

## 版本或 Changelog 不一致

暂停发布，按 consumer-visible 影响重新确认 SemVer。逐项核对实际构建与打包输入，
确保同一发布使用同一版本；生成物应由项目现有生成器重建。Changelog 面向用户说明
收益、风险、breaking change 和迁移要求，不复制原始 commit 列表充数。

如果只获得 preparation 授权，把工作停在本地验证结果；不要 tag、push 或触发
GitHub workflow。

## Tag 错误

先区分 tag 尚未创建、仅存在本地、已推送、或已经产生 Release。不要移动、删除或
重建 tag 来“快速修复”。报告错误 tag、其 commit、远端状态和可选恢复路径；删除
本地/远端 tag、替换 Release 或发布纠正版本都需要用户对该动作的明确授权。

## CI 或构建失败

读取失败 workflow 的当前日志，定位是代码、配置、权限还是发布基础设施问题。保留
失败 run 与 tag 的证据，不自动重复推送或无限重试。若修复会改变已标记 commit，
优先提出新的纠正版本；只有项目策略和用户明确授权时才执行其他恢复方式。

## Release notes 或产物不符

将 GitHub Release 与 `CHANGELOG.md`、workflow 输出和实际上传资产对照。创建发布的
授权可以覆盖该次发布流程中预先说明的 notes 写入；对已经发布的 Release 做后续编辑
仍应明确说明目标和影响。不要假定平台矩阵、资产名称或数量恒定，以当前 workflow
和 run 结果为证据。

## 发布后遗漏

先评估遗漏是否改变用户决策或升级安全。仅文案澄清可提出 notes 修订；代码、构建或
兼容性遗漏通常应提出新的 patch/minor/major 版本。未经授权不编辑旧 Release，也不
自行创建补发 tag。
