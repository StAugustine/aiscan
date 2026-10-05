# reflex-replacement-live-20261005-r3

按实验记录的官方费率与返回用量估算，非账单。每轮独立统计，失败尝试保留。

| 场景 / 组别 | 成功 / 实际运行 | 阻塞 | 编译 USD | 运行 USD | 全部尝试 / 成功任务 USD |
|---|---:|---:|---:|---:|---:|
| async/cold_learning | 3/3 | 0 | 0.011789 | 0.001345 | 0.004378174 |
| async/ordinary_llm | 5/5 | 0 | 0.000000 | 0.002281 | 0.000456292 |
| async/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| async/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| browser/cold_learning | 2/3 | 0 | 0.037119 | 0.004319 | 0.020718735 |
| browser/ordinary_llm | 4/5 | 0 | 0.000000 | 0.005221 | 0.001305255 |
| browser/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| browser/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| repeat/cold_learning | 3/3 | 0 | 0.010507 | 0.001348 | 0.003951606 |
| repeat/ordinary_llm | 5/5 | 0 | 0.000000 | 0.001937 | 0.000387334 |
| repeat/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| repeat/reflex_jev | 0/0 | 5 | — | — | 无法确认 |

已返回用量的费用：$0.075866；缺失用量/价格：0。
未完成替代验收，不计算节省率或回本次数。
