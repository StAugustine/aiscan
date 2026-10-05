# reflex-replacement-live-20261005-r5

按实验记录的官方费率与返回用量估算，非账单。每轮独立统计，失败尝试保留。

| 场景 / 组别 | 成功 / 实际运行 | 阻塞 | 编译 USD | 运行 USD | 全部尝试 / 成功任务 USD |
|---|---:|---:|---:|---:|---:|
| async/cold_learning | 3/3 | 0 | 0.107292 | 0.001454 | 0.036248505 |
| async/ordinary_llm | 5/5 | 0 | 0.000000 | 0.002549 | 0.000509765 |
| async/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| async/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| browser/cold_learning | 3/3 | 0 | 0.172511 | 0.002908 | 0.058472922 |
| browser/ordinary_llm | 4/5 | 0 | 0.000000 | 0.005524 | 0.001380949 |
| browser/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| browser/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| repeat/cold_learning | 3/3 | 0 | 0.150871 | 0.001038 | 0.050636546 |
| repeat/ordinary_llm | 4/5 | 0 | 0.000000 | 0.002044 | 0.000511078 |
| repeat/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| repeat/reflex_jev | 0/0 | 5 | — | — | 无法确认 |

已返回用量的费用：$0.446191；缺失用量/价格：0。
未完成替代验收，不计算节省率或回本次数。
