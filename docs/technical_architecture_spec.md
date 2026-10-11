# Rocateq — 核心业务系统架构与接口技术规范文档
# Technical Architecture & Interface Specification

> **版本**：v1.0.0 | **编制人**：Alex Zhang | **日期**：2026-10-11  
> **适用业务方**：Rocateq Operations (Derrick van Welie) & Commercial (Godert Burghard)  
> **设计目标**：以极高人效（一人可独立开发维护）、轻量敏捷（MVP 快速上线）、低运行成本为核心原则，通过开源成熟底座结合自研 Go 核心微服务与多模态 LLM API，全面支撑 1,000 → 2,000 家超市的规模化扩张。

---

## 目录
0. [业务全景故事线：Derrick 两大核心诉求的有机串联 (The Operational Storyline)](#0-业务全景故事线derrick-两大核心诉求的有机串联-the-operational-storyline)
1. [系统总体设计哲学与技术栈选型](#1-系统总体设计哲学与技术栈选型)
2. [需求一：远程故障诊断与门店自助排障平台 (Remote Diagnostics Platform)](#2-需求一远程故障诊断与门店自助排障平台)
   - 2.1 [端到端架构拓扑图](#21-端到端架构拓扑图)
   - 2.2 [边缘控制器通讯与 MQTT Topic 规范](#22-边缘控制器通讯与-mqtt-topic-规范)
   - 2.3 [设备遥测与状态报文格式 (JSON Schema)](#23-设备遥测与状态报文格式-json-schema)
   - 2.4 [自研 Go 诊断引擎核心设计 (State Machine & Rule Engine)](#24-自研-go-诊断引擎核心设计)
   - 2.5 [LLM 动态多语言排障指引生成机制](#25-llm-动态多语言排障指引生成机制)
   - 2.6 [B端超市极简排障向导 API 规范](#26-b端超市极简排障向导-api-规范)
3. [需求二：内部出差工时智能计算与邮件报销系统 (Smart Travel & Expense ERP)](#3-需求二内部出差工时智能计算与邮件报销系统)
   - 3.1 [无感交互架构拓扑图](#31-无感交互架构拓扑图)
   - 3.2 [n8n 工作流编排与邮件 Webhook 处理流水线](#32-n8n-工作流编排与邮件-webhook-处理流水线)
   - 3.3 [多模态 LLM 发票结构化提取接口设计](#33-多模态-llm-发票结构化提取接口设计)
   - 3.4 [工时与出差打卡极简 API 规范](#34-工时与出差打卡极简-api-规范)
4. [部署拓扑、安全性与基础设施成本估算](#4-部署拓扑安全性与基础设施成本估算)

---

## 0. 业务全景故事线：Derrick 两大核心诉求的有机串联 (The Operational Storyline)

在 Rocateq 的日常运营中，**“远程排障 Dashboard”** 与 **“出差工时/智能报销 ERP”** 并非两个孤立的 IT 项目，而是**一套环环相扣、相互承接的业务运维闭环故事**。

### 0.1 闭环故事演进（The End-to-End Journey）

```mermaid
sequenceDiagram
    autonumber
    actor Store as B端超市店长 (Supermarket)
    participant Dash as 远程排障系统 (Dashboard)
    participant Core as Rocateq 诊断云平台 (Go+EMQX)
    actor Eng as Rocateq 现场工程师
    participant ERP as 智能 ERP 移动端 (PWA)
    participant LLM as 多模态 LLM / n8n 引擎

    Note over Store, Core: 【环节一：故障发生与店端自查分流】
    Store->>Dash: 发现推车防盗系统报警/故障
    Core->>Dash: 捕获遥测并分析根因 (如: 供电正常但线圈阻抗断路)
    Dash-->>Store: 呈现 3 步图文自查向导 ("请检查黄色插头是否松动")
    
    alt 店长自查成功 (80% 琐碎问题)
        Store->>Dash: 插紧电线，点击“重新验证”
        Dash-->>Store: 绿灯恢复正常！(闭环结束，成功拦截一次跨国出差)
    else 自查无法解决 (真实硬件损坏)
        Store->>Dash: 自查未果，点击“请求技术支持”
        Dash->>Core: 自动打包控制器软硬件快照与故障码，生成精准工单
        
        Note over Core, Eng: 【环节二：必要出差与现场工时追踪】
        Core->>Eng: 派单并指明所需备件，工程师携带精确备件飞往当地
        Eng->>ERP: 抵达超市现场，点击“到达现场签入” (开始工时计时)
        Note over Eng: 现场维修、更换损坏部件
        Eng->>ERP: 维修完毕，点击“签出” (自动核算施工与差旅工时，回传总部)
        
        Note over Eng, LLM: 【环节三：傍晚智能发票报销与数据闭环】
        LLM-->>Eng: 当天傍晚自动发送邮件/通知催促提交差旅凭证
        Eng->>ERP: 手机拍照发票 (机票/酒店/餐饮)
        ERP->>LLM: 触发多模态 LLM API 进行自动识别与结构化提取
        LLM-->>ERP: 自动生成报销单项 (金额/币种/税率/商户) 归档入库
    end
```

### 0.2 两大场景的因果逻辑与战略意义

1. **场景一（前置拦截）：因为超市故障频发，所以需要 Remote Dashboard 实现“自查优先”**
   * **历史痛点**：超市 B 端一旦出故障，完全不知道问题在哪，只能恐慌性地催促 Rocateq 派人。由于缺少远程可见性，Rocateq 只能频繁派遣工程师跨国出差，现场却往往发现只是“电线被踢松了、电源插头掉了”等小儿科问题，导致巨大的差旅费与人力浪费。
   * **破局设计**：远程 Dashboard 落地后，让超市店长第一时间通过傻瓜式界面自查排障。**只有当系统判定为硬件损坏、或店长自查确认无法解决时，才会升级派单**。这在前置端直接砍掉了 80% 的无效出差。

2. **场景二（后置提效）：因为自查无法解决才出差，出差现场由智能 ERP 承接“工时与报销自动化”**
   * **落地衔接**：当故障被 Dashboard 确认必须派人时，工程师带着精准定位的备件飞赴现场。
   * **现场工时（Timesheet）**：工程师打开 ERP 移动端（PWA），点击**“到达现场签入”**，开始精准计量服务工时；维修完成点击**“签出”**，数据实时同步总部，彻底告别月末事后回忆式补填。
   * **智能报销（Smart Expense）**：当天傍晚，系统自动化发送邮件提醒。工程师打开界面对发票随手拍照，后台直接调用**多模态 LLM API**自动识别金额、币种、类别并归档进 ERP，出差人员零行政负担。

3. **战略终局：支撑 1,000 → 2,000 家商超扩张的核心杠杆**
   * Derrick 提出：明年门店客户数翻倍（1,000 $\rightarrow$ 2,000），但团队规模锁死在 ~100 人以内。
   * **通过需求一**：消灭 80% 的无谓出差，将全球售后支持量解耦；
   * **通过需求二**：将剩余 20% 真正需要的出差维护流程全面数字化、智能化，把工程师从繁重的贴发票、算工时中解放出来。
   * **两者合力，真正达成“客户翻倍、业务翻倍、但人力不翻倍”的高人效目标。**

---

## 1. 系统总体设计哲学与技术栈选型

### 1.1 核心设计准则
1. **反过度工程（Pragmatic & Low Overhead）**：不盲目搭建重型单体，避免陷入 Java/Spring 或庞大 ERP 框架的维护泥潭。
2. **边缘解耦，容忍弱网（Fault-Tolerant Edge）**：全球商超网络环境复杂，采用 MQTT 协议穿透防火墙并实现离线重连与 QoS 保障。
3. **AI 赋能最后一公里（LLM as UI/UX Bridge）**：传统系统输出生硬的错误码，利用 LLM 将底层诊断动态翻译为非技术店长看得懂的多语种图文操作指引。

### 1.2 技术选型一览
| 层次 | 远程排障系统 (Diagnostics) | 智能报销与工时系统 (ERP Automation) | 选型考量 |
| :--- | :--- | :--- | :--- |
| **消息/集成底座** | **EMQX** (开源 MQTT Broker) | **n8n** (轻量工作流自动化引擎) | 高并发长连接保活；可视化连接器快速打通邮件与 Webhook |
| **业务核心逻辑** | **Go (Golang) 诊断微服务** | **Go / Node.js 极轻微服务** | 内存极小、编译即运行、无运行时依赖、高并发状态机 |
| **智能模型能力** | **Gemini / GPT-4o API** (多语言指引生成) | **Gemini / GPT-4o Vision API** (发票凭证解析) | 准确率远超传统 OCR，零样本适配欧洲多国发票与语言 |
| **存储层** | **PostgreSQL + TimescaleDB** | **PostgreSQL** | 标准关系型存储 + 传感器时序遥测聚合 |
| **前端交互层** | **Grafana** (内部) + **极简 PWA** (超市端) | **响应式 Web PWA / 纯邮件交互** | 内部零开发看大盘；外部极低摩擦，零安装门槛 |

---

## 2. 需求一：远程故障诊断与门店自助排障平台

### 2.1 端到端架构拓扑图

```mermaid
flowchart TD
    subgraph Edge ["边缘门店 (Store Hardware)"]
        Controller["Rocateq Store Controller / Gateway"]
        Sensors["RF Loop / Brake Antenna / Power Supply"]
        Sensors -->|"硬件电压/回路阻抗检测"| Controller
    end

    subgraph Ingestion ["接入与传输层"]
        EMQX["EMQX MQTT Broker (Port 8883 TLS)"]
        Controller -->|"MQTT Telemetry & Events"| EMQX
    end

    subgraph CoreEngine ["核心处理与智能化"]
        GoEngine["自研 Go 诊断引擎 (Diagnostics Engine)"]
        TimescaleDB[("PostgreSQL / TimescaleDB")]
        LLM["LLM API (Prompt Engine)"]
        
        EMQX -->|"Webhook / MQTT Shared Sub"| GoEngine
        GoEngine -->|"时序数据 & 状态入库"| TimescaleDB
        GoEngine -->|"错误码+硬件拓扑"| LLM
        LLM -->|"多语言排障指引 JSON"| GoEngine
    end

    subgraph Frontend ["双端分流呈现"]
        StoreUI["B端超市店长端 (极简 3 步排障 PWA)"]
        InternalGrafana["Rocateq 内部运营大盘 (Grafana)"]
        
        GoEngine -->|"REST API (向导/红绿灯状态)"| StoreUI
        TimescaleDB -->|"SQL 仪表盘"| InternalGrafana
    end
```

---

### 2.2 边缘控制器通讯与 MQTT Topic 规范

采用分级清晰的 MQTT Topic 设计，支持多租户隔离与单门店控制：

* **遥测上报 (Telemetry)**：  
  `rocateq/store/{store_id}/controller/{controller_id}/telemetry`  
  *频次*：正常时每 30 秒心跳一次；检测到状态突变时立即触发。
* **紧急事件上报 (Events / Alarms)**：  
  `rocateq/store/{store_id}/controller/{controller_id}/events`  
  *频次*：突发事件（如天线断路、供电异常）即时推送，QoS = 1。
* **云端指令下发 (Commands)**：  
  `rocateq/store/{store_id}/controller/{controller_id}/commands`  
  *用途*：下发远程复位、触发硬件主动回传自检信号。

---

### 2.3 设备遥测与状态报文格式 (JSON Schema)

#### 遥测报文：`telemetry`
```json
{
  "protocol_version": "1.0",
  "store_id": "NL-JUMBO-1042",
  "controller_id": "CTRL-UNIT-01",
  "timestamp": 1775988000,
  "firmware_version": "v2.4.1",
  "power": {
    "voltage_mv": 12150,
    "current_ma": 420,
    "status": "NORMAL"
  },
  "peripherals": {
    "perimeter_loop_1": {
      "impedance_ohms": 99999,
      "status": "OPEN_CIRCUIT",
      "signal_tx_active": false
    },
    "checkout_antenna_2": {
      "impedance_ohms": 52,
      "status": "CONNECTED",
      "signal_tx_active": true
    },
    "cctv_trigger_relay": {
      "status": "CLOSED"
    }
  },
  "ambient": {
    "temp_celsius": 24.5
  }
}
```

---

### 2.4 自研 Go 诊断引擎核心设计

Go 诊断服务维护每个控制器的轻量级内存状态机，并在检测到回路断开时执行规则聚合：

```go
package diagnostics

import "time"

// 诊断根因代码枚举
type RootCauseCode string

const (
    RootCauseOk               RootCauseCode = "SYS_NORMAL"
    RootCauseLoopDisconnected RootCauseCode = "LOOP_CABLE_DISCONNECTED" // 电线松动/插头脱落
    RootCausePowerFailure     RootCauseCode = "POWER_SUPPLY_ABNORMAL"   // 供电电压不稳
    RootCauseAntennaFault     RootCauseCode = "ANTENNA_TX_FAULT"        // 发射端硬件损坏
    RootCauseNetworkOffline   RootCauseCode = "GATEWAY_OFFLINE"         // 网关完全失联
)

// 诊断引擎核心决策函数
func EvaluateTelemetry(t *TelemetryPayload) DiagnosticResult {
    // 典型场景：电源供电正常，但地面感应线圈阻抗为无穷大 (OPEN_CIRCUIT)
    if t.Power.Status == "NORMAL" && t.Peripherals.PerimeterLoop1.Status == "OPEN_CIRCUIT" {
        return DiagnosticResult{
            Severity:      "HIGH",
            Code:          RootCauseLoopDisconnected,
            TargetModule:  "Perimeter Loop Connector J2",
            IsSelfFixable: true, // 超市店长可自行修复（无需派遣跨国工程师）
            ActionHint:    "Check physical screw-terminal or plug at Port J2 behind Master Control Box.",
            Timestamp:     time.Now(),
        }
    }
    
    // 供电异常判断
    if t.Power.VoltageMv < 10000 {
        return DiagnosticResult{
            Severity:      "CRITICAL",
            Code:          RootCausePowerFailure,
            TargetModule:  "Power Adapter Unit",
            IsSelfFixable: true,
            ActionHint:    "Check power adapter outlet connection or fuse.",
            Timestamp:     time.Now(),
        }
    }

    return DiagnosticResult{Code: RootCauseOk, Severity: "INFO", IsSelfFixable: false}
}
```

---

### 2.5 LLM 动态多语言排障指引生成机制

当 Go 引擎判定为 `IsSelfFixable: true` 时，调用 LLM API，根据该超市客户的偏好语言（如 Albert Heijn 传 `nl`，Carrefour 传 `fr`，Walmart 传 `en`），动态输出**非技术人员可理解的三步向导 JSON**：

#### LLM Prompt 结构
```text
System: You are a helpful B2B store maintenance wizard for Rocateq retail anti-theft systems.
Input Context:
- Store Language: nl (Dutch)
- Detected Fault: LOOP_CABLE_DISCONNECTED
- Hardware Model: Rocateq Controller Box Pro v2
- Target Component: Terminal Connector J2 (Yellow plug)
Task: Output a concise, friendly, 3-step action guide for a non-technical supermarket manager to verify and plug the loose wire back in. Return pure JSON.
```

#### LLM 输出给前端渲染的标准化 JSON
```json
{
  "title": "Winkelwagentjesbeveiliging vereist inspectie",
  "status_color": "RED",
  "estimated_fix_time": "2 minuten",
  "steps": [
    {
      "step_number": 1,
      "title": "Kast lokaliseren",
      "instruction": "Open de hoofdbesturingskast van Rocateq onder kassa 1.",
      "visual_hint_icon": "cabinet_open"
    },
    {
      "step_number": 2,
      "title": "Gele kabelpoort (J2) controleren",
      "instruction": "Controleer of de gele kabelstekker aan poort J2 goed vastzit. Druk deze stevig aan tot u een klik hoort.",
      "visual_hint_icon": "cable_plug"
    },
    {
      "step_number": 3,
      "title": "Systeem opnieuw testen",
      "instruction": "Druk hieronder op 'Opnieuw testen'. Als het lampje groen wordt, is het systeem direct weer hersteld.",
      "visual_hint_icon": "refresh_check"
    }
  ]
}
```

---

### 2.6 B端超市极简排障向导 API 规范

#### 1. 获取门店整体运行状态与排障向导
* **Endpoint**: `GET /api/v1/stores/{store_id}/diagnostics/wizard`
* **Response (200 OK)**:
```json
{
  "store_id": "NL-JUMBO-1042",
  "system_status": "ACTION_REQUIRED",
  "active_issue": {
    "issue_id": "ISSUE-20261011-0089",
    "detected_at": "2026-10-11T14:32:00Z",
    "title": "Boundary Cable Disconnected",
    "can_self_resolve": true,
    "wizard_data": {
      "steps": [
        { "step": 1, "text": "Locate Rocateq Box under Checkout lane 1." },
        { "step": 2, "text": "Re-insert the yellow connector firmly into socket J2." },
        { "step": 3, "text": "Click 'Verify Solution' below." }
      ]
    }
  }
}
```

#### 2. 超市店长插好线后触发“即时远程验证”
* **Endpoint**: `POST /api/v1/stores/{store_id}/diagnostics/wizard/verify`
* **Payload**:
```json
{
  "issue_id": "ISSUE-20261011-0089",
  "controller_id": "CTRL-UNIT-01"
}
```
* **后端动作**：Go 服务向 MQTT 下发 `rocateq/store/.../commands` 发起实时检测；等待 3 秒捕获新遥测。
* **Response (200 OK)**:
```json
{
  "result": "RESOLVED",
  "message": "Loop impedance normal. Green status restored!",
  "current_status": "HEALTHY"
}
```

#### 3. 实在搞不定？一键升级精准派单 (Qualified Dispatch)
* **Endpoint**: `POST /api/v1/stores/{store_id}/tickets/escalate`
* **说明**：自动将控制器遥测快照、照片记录、型号打包生成工单，通知 Rocateq 售后，若需派工出差，派单系统已明确指明所需更换备件。

---

## 3. 需求二：内部出差工时智能计算与邮件报销系统

### 3.1 无感交互架构拓扑图

```mermaid
flowchart LR
    Engineer["现场工程师 (在海外出差)"]
    
    subgraph EmailPath ["邮件极简报销 (零学习成本)"]
        MailServer["公司邮箱 / IMAP (Outlook/Google)"]
        n8n["n8n 自动化引擎 (Email Trigger)"]
        LLMVision["Gemini / GPT-4o Vision API"]
        
        Engineer -->|"1. 手机回复邮件 + 附带发票照片"| MailServer
        MailServer -->|"2. 提取附件图片"| n8n
        n8n -->|"3. 发票凭证结构化抽取"| LLMVision
    end

    subgraph MobilePWA ["工时智能计算 (移动端 PWA)"]
        PWA["轻量移动打卡 PWA"]
        Engineer -->|"一键抵达现场/离开现场"| PWA
    end

    subgraph Backend ["业务落地与核算"]
        GoERP["Go ERP 业务服务"]
        Postgres[("PostgreSQL")]
        ExternalAccounting["现有财务软件 (Exact/Twinfield)"]
        
        LLMVision -->|"4. 结构化 JSON"| n8n
        n8n -->|"5. 记录报销单"| GoERP
        PWA -->|"工时与打卡数据"| GoERP
        GoERP --> Postgres
        GoERP -.->|"定期同步"| ExternalAccounting
    end
```

---

### 3.2 n8n 工作流编排与邮件处理流水线

* **节点 1：定时触发催促（Cron Trigger）**  
  每逢周五下午 16:00，扫描当前处于出差状态（In-Trip）的工程师列表，自动发送邮件：
  > *"Hi Mark, your trip to Paris ends today. Please simply reply to this email with photos of your travel receipts (hotels, meals, train tickets)."*
* **节点 2：邮件回复监听（IMAP / Microsoft 365 Webhook）**  
  监测到主题包含 `Re: [Trip-... Expense Reminder]` 的回信，提取发件人邮箱与图片附件。
* **节点 3：并发调用 LLM Vision 提取**  
  将图片流传给多模态 LLM 进行凭证解析。
* **节点 4：写入报销数据库并回执**  
  录入成功后自动回复工程师邮件：  
  > *"Received 3 receipts (€142.50 Total). Your expense claim has been logged for approval."*

---

### 3.3 多模态 LLM 发票结构化提取接口设计

#### 传给多模态模型的 Prompt 指令规范
```text
System: You are an automated European accounting receipt parser for Rocateq.
Task: Extract financial data from the attached receipt image into strict JSON.
Rules:
- Automatically identify currency (EUR, USD, GBP, etc.).
- Convert amounts to standard decimal notation.
- Categorize into one of: ["HOTEL", "TRANSPORT_FLIGHT_TRAIN", "TAXI", "MEAL", "TOOLS_SUPPLIES", "OTHER"].
- If total or date is unreadable, set "flag_manual_review": true.
```

#### 标准化提取响应结构
```json
{
  "merchant_name": "SNCF TGV INOUI",
  "transaction_date": "2026-10-09",
  "currency": "EUR",
  "total_amount": 89.20,
  "vat_amount": 8.11,
  "expense_category": "TRANSPORT_FLIGHT_TRAIN",
  "confidence_score": 0.98,
  "flag_manual_review": false,
  "line_items_summary": "Train Ticket Paris Gare du Nord -> Rotterdam Centraal"
}
```

---

### 3.4 工时与出差打卡极简 API 规范

#### 1. 工程师出差打卡 (Check-in / Check-out)
* **Endpoint**: `POST /api/v1/engineers/timesheets/punch`
* **Payload**:
```json
{
  "engineer_id": "ENG-ALEX-01",
  "project_trip_id": "TRIP-2026-PARIS-02",
  "store_id": "FR-CARREFOUR-7712",
  "action": "CHECK_IN", // or "CHECK_OUT"
  "timestamp": 1775988000,
  "geo_lat": 48.8566,
  "geo_lng": 2.3522
}
```
* **业务逻辑**：Go 服务自动根据 `CHECK_IN` 和 `CHECK_OUT` 计算现场施工工时，并结合出差行程表自动累加“海外交通时间工时系数”。

#### 2. 出差报销汇总与工时审阅（供 Derrick/财务查看）
* **Endpoint**: `GET /api/v1/trips/{trip_id}/summary`
* **Response (200 OK)**:
```json
{
  "trip_id": "TRIP-2026-PARIS-02",
  "engineer_name": "Mark de Boer",
  "destination": "Paris, France",
  "total_travel_hours": 16.5,
  "total_onsite_hours": 24.0,
  "expenses": {
    "total_eur": 420.50,
    "receipt_count": 5,
    "receipts_pending_review": 0
  },
  "status": "READY_FOR_APPROVAL"
}
```

---

## 4. 部署拓扑、安全性与基础设施成本估算

### 4.1 单节点全容器化部署架构（Docker Compose 即可支撑 2,000 家门店）

得益于 Go 的极致低资源开销与 EMQX 的轻量级，初期无需上沉重的 Kubernetes，单台 4核 8G 的 Linux 云主机（Hetzner / AWS Lightsail / DigitalOcean）即可承载全套业务：

```yaml
version: '3.8'

services:
  emqx:
    image: emqx/emqx:5.8.0
    ports:
      - "1883:1883"   # MQTT TCP
      - "8883:8883"   # MQTT TLS
      - "8083:8083"   # MQTT WSS
      - "18083:18083" # EMQX Dashboard
    volumes:
      - emqx_data:/opt/emqx/data

  diagnostics-engine:
    build: ./services/diagnostics-go
    environment:
      - MQTT_BROKER=tcp://emqx:1883
      - DB_DSN=postgres://rocateq:secret@timescaledb:5432/telemetry
      - LLM_API_KEY=${LLM_API_KEY}
    depends_on:
      - emqx
      - timescaledb

  n8n:
    image: docker.n8n.io/n8nio/n8n
    ports:
      - "5678:5678"
    environment:
      - WEBHOOK_URL=https://workflow.rocateq.com/
      - GENERIC_TIMEZONE=Europe/Amsterdam
    volumes:
      - n8n_data:/home/node/.n8n

  timescaledb:
    image: timescale/timescaledb:latest-pg16
    ports:
      - "5432:5432"
    volumes:
      - db_data:/var/lib/postgresql/data

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - grafana_data:/var/lib/grafana

volumes:
  emqx_data:
  n8n_data:
  db_data:
  grafana_data:
```

### 4.2 安全与合规设计 (Security & Privacy)
1. **MQTT 链路安全**：采用 TLS 1.3 双向加密，每台门店控制器内置唯一客户端证书或 Device Token，禁止跨门店订阅。
2. **欧洲 GDPR 隐私合规**：发票解析照片在提取结构化金额后，敏感人脸/个人住址通过 LLM 提示词进行清洗过滤，不保留多余个人数据。

### 4.3 月度基础设施运营成本预估 (Monthly OPEX)
| 基础设施项 | 规格/服务商 | 预估月费用 (EUR) |
| :--- | :--- | :--- |
| **云服务器计算节点** | 4 vCPU, 8GB RAM, 160GB SSD (如 Hetzner CX32) | ~€15 / 月 |
| **EMQX & 内部中间件** | 开源自建（完全免费） | €0 |
| **LLM API 调用成本** | 2,000 家店每日少量排障 + 工程师发票识别 (按按量计费) | ~€10 - €25 / 月 |
| **云端备份与对象存储** | S3 兼容备份存储 50GB | ~€2 / 月 |
| **合计运营成本** | **支撑 2,000 家商超客户的云基础设施成本** | **< €50 / 月** |

> **商业收益对比**：消灭**单次**从荷兰前往法国或德国现场查看“松动电线”的差旅费（机票 + 酒店 + 工程师工时成本通常在 **€1,500 – €3,000 / 次**），整套系统的月度运行成本甚至不足单次出差的零头，投资回报率（ROI）极其惊人。

