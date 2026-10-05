# reflex-replacement-live-20261005-r7

按实验记录的官方费率与返回用量估算，非账单。每轮独立统计，失败尝试保留。

| 场景 / 组别 | 成功 / 实际运行 | 阻塞 | 编译 USD | 运行 USD | 全部尝试 / 成功任务 USD |
|---|---:|---:|---:|---:|---:|
| async/cold_learning | 3/3 | 0 | 0.109909 | 0.001337 | 0.037082175 |
| async/ordinary_llm | 5/5 | 0 | 0.000000 | 0.002258 | 0.00045161 |
| async/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| async/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| browser/cold_learning | 3/3 | 0 | 0.107132 | 0.002397 | 0.036509888 |
| browser/ordinary_llm | 5/5 | 0 | 0.000000 | 0.004230 | 0.000846014 |
| browser/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| browser/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| repeat/cold_learning | 3/3 | 0 | 0.087036 | 0.001306 | 0.029447356 |
| repeat/ordinary_llm | 5/5 | 0 | 0.000000 | 0.001984 | 0.000396802 |
| repeat/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| repeat/reflex_jev | 0/0 | 5 | — | — | 无法确认 |

已返回用量的费用：$0.317590；缺失用量/价格：0。
未完成替代验收，不计算节省率或回本次数。
