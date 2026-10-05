# reflex-replacement-live-20261005-r2

按实验记录的官方费率与返回用量估算，非账单。每轮独立统计，失败尝试保留。

| 场景 / 组别 | 成功 / 实际运行 | 阻塞 | 编译 USD | 运行 USD | 全部尝试 / 成功任务 USD |
|---|---:|---:|---:|---:|---:|
| async/cold_learning | 0/3 | 0 | 0.007837 | 0.001264 | 无法确认 |
| async/ordinary_llm | 2/5 | 0 | 0.000000 | 0.002896 | 0.001447881 |
| async/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| async/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| browser/cold_learning | 3/3 | 0 | 0.020825 | 0.002376 | 0.007733442 |
| browser/ordinary_llm | 5/5 | 0 | 0.000000 | 0.005021 | 0.001004101 |
| browser/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| browser/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| repeat/cold_learning | 2/3 | 0 | 0.009616 | 0.001503 | 0.005559381 |
| repeat/ordinary_llm | 3/5 | 0 | 0.000000 | 0.007440 | 0.002480116 |
| repeat/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| repeat/reflex_jev | 0/0 | 5 | — | — | 无法确认 |

已返回用量的费用：$0.058777；缺失用量/价格：0。
未完成替代验收，不计算节省率或回本次数。
