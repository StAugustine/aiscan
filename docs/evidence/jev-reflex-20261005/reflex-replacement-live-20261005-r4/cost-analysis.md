# reflex-replacement-live-20261005-r4

按实验记录的官方费率与返回用量估算，非账单。每轮独立统计，失败尝试保留。

| 场景 / 组别 | 成功 / 实际运行 | 阻塞 | 编译 USD | 运行 USD | 全部尝试 / 成功任务 USD |
|---|---:|---:|---:|---:|---:|
| async/cold_learning | 3/3 | 0 | 0.011759 | 0.001372 | 0.004377105 |
| async/ordinary_llm | 5/5 | 0 | 0.000000 | 0.002391 | 0.000478254 |
| async/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| async/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| browser/cold_learning | 3/3 | 0 | 0.024451 | 0.003060 | 0.009170182 |
| browser/ordinary_llm | 3/5 | 0 | 0.000000 | 0.005275 | 0.001758428 |
| browser/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| browser/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| repeat/cold_learning | 3/3 | 0 | 0.008637 | 0.001041 | 0.003226274 |
| repeat/ordinary_llm | 5/5 | 0 | 0.000000 | 0.001868 | 0.000373681 |
| repeat/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| repeat/reflex_jev | 0/0 | 5 | — | — | 无法确认 |

已返回用量的费用：$0.059856；缺失用量/价格：0。
未完成替代验收，不计算节省率或回本次数。
