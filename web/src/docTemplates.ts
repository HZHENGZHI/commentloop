export interface DocTemplate {
  id: string;
  name: string;
  description: string;
  content: string;
}

export const DOC_TEMPLATES: DocTemplate[] = [
  {
    id: "blank",
    name: "空白文档",
    description: "从零开始写",
    content: "",
  },
  {
    id: "prd",
    name: "PRD",
    description: "目标、范围、验收标准",
    content: `# 产品需求

## 背景

## 目标

## 范围

## 非目标

## 验收标准

## 风险与依赖
`,
  },
  {
    id: "adr",
    name: "ADR",
    description: "架构决策记录",
    content: `# 架构决策记录

## 背景

## 决策

## 备选方案

## 影响

## 迁移与回滚
`,
  },
  {
    id: "weekly",
    name: "周报",
    description: "结果、风险、下周计划",
    content: `# 周报

## 本周结果

## 数据与反馈

## 风险与阻塞

## 下周计划
`,
  },
];
