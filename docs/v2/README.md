# HotPlex 2.0 文档目录

本目录集中存放 HotPlex 2.0 的产品定位、架构设计、API 契约、实施路线和 GitHub 研发跟踪文档。

## 文档索引

| 文档 | 作用 |
| --- | --- |
| [ROADMAP.md](ROADMAP.md) | 2.0 产品定位、阶段路线、成功指标和参考资料 |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 2.0 Runtime Gateway 架构边界和目标组件 |
| [API-DESIGN.md](API-DESIGN.md) | AgentSpec、AgentIdentity、runtime events、metadata 和候选 API |
| [IMPLEMENTATION-ROADMAP.md](IMPLEMENTATION-ROADMAP.md) | 高 ROI first cuts、产品迭代波次和实施门禁 |
| [GITHUB-MILESTONES.md](GITHUB-MILESTONES.md) | GitHub milestones、issues、labels 与验收标准 |

## 维护原则

- 2.0 定位为 self-hosted Agent Runtime Gateway。
- 优先演进现有 Gateway、Session、Worker、AEP、Audit、Observability、EventStore。
- 暂缓分布式 scheduler、独立 memory service、Agent marketplace、复杂策略语言和多 Agent workflow。
- 修改 2.0 路线时，同步检查 GitHub issues #847-#852 是否仍与文档一致。
