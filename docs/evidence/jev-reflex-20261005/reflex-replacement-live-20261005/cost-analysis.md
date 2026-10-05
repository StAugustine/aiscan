# reflex-replacement-live-20261005

按实验记录的官方费率与返回用量估算，非账单。每轮独立统计，失败尝试保留。

| 场景 / 组别 | 成功 / 实际运行 | 阻塞 | 编译 USD | 运行 USD | 全部尝试 / 成功任务 USD |
|---|---:|---:|---:|---:|---:|
| async/cold_learning | 1/3 | 0 | 0.007699 | 0.000939 | 0.008637858 |
| async/ordinary_llm | 3/5 | 0 | 0.000000 | 0.005320 | 0.001773248 |
| async/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| async/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| browser/cold_learning | 3/3 | 0 | 0.027195 | 0.003363 | 0.010186166 |
| browser/ordinary_llm | 3/5 | 0 | 0.000000 | 0.007249 | 0.002416402 |
| browser/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| browser/reflex_jev | 0/0 | 5 | — | — | 无法确认 |
| repeat/cold_learning | 1/3 | 0 | 0.026350 | 0.004834 | 0.031183386 |
| repeat/ordinary_llm | 3/5 | 0 | 0.000000 | 0.008440 | 0.002813416 |
| repeat/reflex_llm_judge | 0/0 | 5 | — | — | 无法确认 |
| repeat/reflex_jev | 0/0 | 5 | — | — | 无法确认 |

已返回用量的费用：$0.091389；缺失用量/价格：0。
未完成替代验收，不计算节省率或回本次数。
