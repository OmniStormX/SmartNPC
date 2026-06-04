---
name: iwiki
description: "用于访问企业内部 iWiki（iwiki.woa.com）的专用 skill。只要用户问题中出现 iwiki/iWiki 关键词，或用户提供的链接 host 为 iwiki.woa.com，就应优先调用本 skill，而不是使用其它工具或技能替代。本 skill 支持：获取iwiki文档全文与元数据（作者/时间等）、搜索iwiki文档内容（可按空间/专题限定）、获取iwiki空间信息与目录树、查询/维护文档标签、查看/添加评论、查询引用关系（引用/被引用）、列出文档图片、获取附件下载地址，以及创建/保存/局部更新/移动文档等读写操作。"
homepage: https://prod.mcp.it.woa.com/app_iwiki_mcp/mcp3
metadata:
  knot:
    category: tencent
    requires:
      mcporter:
        server_name: iwiki
        url: https://prod.mcp.it.woa.com/app_iwiki_mcp/mcp3
        auth: taihu_token
  version: "0.2.1"
---

# iwiki MCP 服务

## ⚠️ 重要提醒：必须通过 mcporter-internal 调用

**此 MCP 服务不能直接调用**，必须通过 `mcporter-internal` 这个 MCP 客户端工具来访问：

```
✅ 正确方式：mcporter-internal call iwiki.<工具名>
❌ 错误方式：直接调用 MCP 工具（会失败）
```

**调用示例**：
```bash
# 检查服务状态
mcporter-internal list

# 调用工具（Mac/Linux 优先使用 --args JSON 格式传参；Windows 因 JSON 解析问题可改用 kv 方式）
# Mac/Linux：
mcporter-internal call iwiki getDocument --args '{"docid": "123456"}'
mcporter-internal call iwiki searchDocument --args '{"query": "关键词"}'
mcporter-internal call iwiki aiSearchDocument --args '{"query": "搜索内容", "limit": 10}'
# Windows（kv 方式，当参数值为数字形式的 ID/编码时加 --raw-strings 防止被转为 number）：
# mcporter-internal call iwiki getDocument docid=123456 --raw-strings
# mcporter-internal call iwiki searchDocument query=关键词 --raw-strings
```

> 💡 不熟悉 mcporter-internal 用法？请参考 `mcporter-internal` skill 文档。若当前环境未加载该 skill，可查阅本目录下的 `DEPENDENCIES.md`。

---

## 通用说明

### 参数约定

| 参数 | 说明 |
|------|------|
| `docid` | 文档ID，文档唯一标识符 |
| `spaceid` | 空间ID |
| `parentid` | 父级文档ID |
| `start/pageIndex/pageNum` | 分页起始位置/页码 |
| `limit/pageSize` | 每页数量 |

### 调用方式

```bash
# ✅ Mac/Linux 优先：使用 --args JSON 格式，确保参数类型正确
mcporter-internal call iwiki <tool_name> --args '{"key": "value"}'

# ✅ Windows 可用 kv 方式（避免 JSON 在终端的解析问题）
# 当参数值为数字形式的 ID/编码时加 --raw-strings 防止被转为 number（如 docid=12345 保持 "12345"）
mcporter-internal call iwiki <tool_name> key=value --raw-strings
```

---

## 工具列表

### 文档查询

#### `getDocument` - 获取文档内容
获取iWiki文档内容，返回文档的完整Markdown格式内容，包括标题、正文、图片、表格等所有元素

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | string | ✓ | iWiki文档ID，文档的唯一标识符 |
| raw | boolean | | 是否启用原生模式，true 时直接返回文档原始 pm-json 内容，默认 false |

#### `metadata` - 获取文档元数据
获取iWiki文档元数据，包含文档的创建时间、修改标题、时间、作者等基础属性数据

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | string | ✓ | iWiki文档ID，文档的唯一标识符 |

#### `aiSearchDocument` - AI搜索文档内容
适用于AI搜索iWiki文档内容，通过传入的query搜索出相关的内容片段，用于深度内容查找和知识发现

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| query | string | ✓ | 要搜索的内容，支持关键词和短语 |
| limit | number | | 搜索的内容数量，默认 5，如果模型允许 10 个效果更好 |
| space_ids | string | | 支持搜索多个空间的内容 |
| topic_ids | string | | 可以搜索专题的多种内容来源，topic id 用逗号分割（如 `https://iwiki.woa.com/topic/123` 中的 `123`） |
| doc_objs | array | | 指定搜索的文档对象列表，用于限定搜索范围到特定文档或文件夹 |

#### `searchDocument` - 关键词搜索文档
传统关键词搜索iWiki内容，支持按类型、空间、标签、作者等条件进行过滤搜索

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| query | string | ✓ | 要搜索的关键词内容 |
| search_type | array | | 搜索类型：space/comment/attachment/page/all，支持多选 |
| space_id | array | | 空间ID数组，限定在指定空间内搜索 |
| tags | string | | 标签，多个标签用英文逗号分隔 |
| author | array | | 作者数组，限定搜索指定作者的内容 |
| offset | number | | 分页偏移量，用于从指定位置开始返回搜索结果 |
| stime | string | | 搜索时间范围的开始时间，格式：`2026-01-01 00:00:00`，需与 `etime` 同时指定 |
| etime | string | | 搜索时间范围的结束时间，格式：`2026-01-01 00:00:00`，需与 `stime` 同时指定 |

#### `getDocQuoteList` - 获取文档引用列表
返回当前文档引用的所有其他文档列表，用于了解文档的引用来源和知识关联

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | string | ✓ | iWiki文档ID |
| start | number | | 起始位置，默认 1 |
| limit | number | | 文档数量，默认 10 |

#### `getDocQuoteListBy` - 获取文档被引用列表
返回引用了当前文档的所有其他文档列表，用于分析文档间的引用关系和影响范围

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | string | ✓ | iWiki文档ID |
| start | number | | 起始位置，默认 1 |
| limit | number | | 文档数量，默认 10 |

#### `listImages` - 获取文档图片列表
解析文档内容提取所有图片的附件ID，返回图片引用列表

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | string | ✓ | iWiki文档ID |

#### `getAttachmentDownloadUrl` - 获取附件下载地址
获取附件的临时下载URL，iWiki附件包含图片、文件等各种类型的资源

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| attachmentid | string | ✓ | 附件ID，可从文档内容或图片列表中获取 |

---

### 空间查询

#### `getSpacePageTree` - 获取空间目录树
返回指定父级文档下的所有子文档列表，包含文档ID、标题、父级ID和是否有子文档等信息

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| parentid | number | ✓ | 父级的文档ID，作为获取子文档的起始节点 |

#### `getSpaceInfoByKey` - 根据 Key 查询空间
根据空间Key查询空间相关信息，返回空间的基本信息，包括空间ID、名称、描述、创建者、权限设置等

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| spaceKey | string | ✓ | 空间 Key（如 `~username` 或 `woa`） |

> 示例：个人空间 `https://iwiki.woa.com/space/~myname`，key 为 `~myname`；其他空间 `https://iwiki.woa.com/space/woa`，key 为 `woa`

#### `getSpaceInfoByName` - 根据名称查询空间
根据空间名查询空间相关信息，支持中文名称或英文名称查询

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| spaceName | string | ✓ | 空间名称或空间的英文名，支持模糊匹配 |

#### `getFavoriteSpaces` - 获取收藏的空间
获取当前用户收藏的空间列表，返回空间ID和空间名称

*无参数*

#### `getManageSpaces` - 获取管理的空间
获取当前用户管理的空间列表，返回空间ID和空间名称

*无参数*

---

### 文档创建/编辑

#### `createDocument` - 创建文档
在指定空间和父级文档下创建新文档，支持多种内容类型

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| spaceid | number | ✓ | iWiki空间ID |
| parentid | number | ✓ | 父级文档ID，指定新文档的层级位置 |
| title | string | ✓ | 文档标题 |
| contenttype | string | | DOC/MD/FOLDER/VIKA，默认 MD |
| body_mode | string | | 兼容旧参数，内容转换为 JSON 后通常无需再传 |
| is_html | boolean | | 是否按 HTML 解析 body，true 使用 HtmlTransformer，false 使用 MarkdownTransformer |
| body | string | | 文档内容 |

#### `saveDocument` - 保存文档
保存或更新 iWiki 文档，修改现有文档的标题或内容

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | number | ✓ | 文档ID |
| title | string | ✓ | 文档标题 |
| is_html | boolean | | 是否按 HTML 解析 body；仅当目标文档是 DOC 类型时生效 |
| raw | boolean | | 是否启用原生模式，true 时直接保存 pm-json 格式内容 |
| body | string | | 文档内容，如不提供则保持原内容不变 |

#### `saveDocumentParts` - 局部更新文档
支持在文档开头插入内容或在文档结尾追加内容

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| id | number | ✓ | 文档ID |
| title | string | ✓ | 文档标题 |
| before | string | | 插入到文档开始的内容 |
| after | string | | 追加到文档结尾的内容 |

> 如果是文档(DOC)类型可以追加HTML格式的富文本内容，markdown格式的可以追加markdown格式的文本

#### `moveDocument` - 移动文档
移动 iWiki 文档到新的位置，支持改变文档的父目录或在同一父目录下调整文档顺序

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | number | ✓ | 要移动的文档ID |
| new_parentid | number | ✓ | 新的父目录ID，如果父目录不变则传 0 |
| position | string | | append/below/above，默认 append |
| target_docid | number | | 目标文档ID（position 为 below/above 时填写） |

#### `copyDocument` - 复制文档
复制 iWiki 文档到新的位置，支持复制单个文档或整个文档树

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | number | ✓ | 要复制的文档ID |
| new_parentid | number | ✓ | 新的父目录ID，复制后的文档将放在此目录下 |
| is_single | number | | 是否只复制单个文档，1 表示只复制当前文档，0 表示复制整个文档树，默认 1 |
| language | string | | 语言设置，默认 zh_CN |

#### `getAttachmentPresignUrl` - 获取附件上传预签名 URL
获取附件上传的预签名 URL，支持文档附件和 Vika 多维表格附件两种模式

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | number | | 文档ID，用于文档附件模式 |
| task_type | string | | 任务类型，用于 Vika 附件模式，支持 `vika_attachment_import` |

#### `notifyAttachmentUpload` - 通知附件上传完成
客户端使用预签名 URL 上传文件到 COS 后，调用此接口完成附件与文档绑定

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| attachmentid | number | ✓ | 附件ID，从 `getAttachmentPresignUrl` 返回的 `attachment_id` 获取 |
| filepath | string | ✓ | 文件名，上传文件的原始名称，包含扩展名 |

#### `renameDocumentTitle` - 重命名文档标题
仅修改 iWiki 文档标题，不修改正文内容

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| id | number | ✓ | 文档ID |
| new_title | string | ✓ | 修改后的新文档标题 |

---

### 词库查询

#### `glossaryTermSearch` - 词条搜索
根据关键词搜索词库中的词条信息，返回匹配的词条列表

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| keyword | string | ✓ | 搜索关键词，用于匹配词条名称或别名 |
| limit | number | | 返回结果数量限制，默认 10 |
| glossary_ids | array | | 词库 ID 列表，用于限定搜索范围 |

#### `glossaryTermExactSearch` - 词条精确搜索
根据关键词精确匹配词库中的词条信息（忽略大小写）

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| keyword | string | ✓ | 搜索关键词，用于精确匹配词条名称或别名 |
| limit | number | | 返回结果数量限制，默认 10 |
| glossary_ids | array | | 词库 ID 列表，用于限定搜索范围 |

#### `glossaryBatchExactSearch` - 批量精确搜索词条
根据多个关键词批量精确匹配词库中的词条信息（忽略大小写）

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| keywords | array | ✓ | 关键词列表，不能为空，最多 100 个 |
| status | string | | 词条状态，逗号分隔，默认 current；可选 current/draft/deleted |
| include_aliases | boolean | | 是否搜索别名，默认 true |
| glossary_ids | array | | 词库 ID 列表，用于限定搜索范围 |

---

### 标签管理

#### `getDocumentTags` - 获取文档标签
获取iWiki文档的标签信息，返回文档所有关联的标签列表

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | string | ✓ | iWiki文档ID |

#### `addDocumentTags` - 添加文档标签
为一个或多个iWiki文档添加标签，支持批量设置多个标签

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docids | array | ✓ | 需要打标签的文档ID列表（number数组，JSON schema 中为 array 类型） |
| labels | array | ✓ | 要添加的标签名称列表（string数组） |

#### `deleteDocumentTag` - 删除文档标签

⚠️ **安全提示**: 删除后无法恢复，不允许批量删除

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | number | ✓ | 文档ID |
| labelName | string | ✓ | 标签名称 |

---

### 评论管理

#### `getComments` - 获取文档评论
获取文档的评论列表，包含评论内容、作者、时间等信息，每页 10 条

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | number | ✓ | 文档ID |
| pageIndex | number | | 页码，默认 1 |

> `next_level_comments` 字段为下一层的回复评论，可根据 total 除以 10 计算总页数

#### `getInlineComment` - 获取划词批注
获取文档的划词批注（内联评论），返回批注ID、评论内容、创建者、回复对象、划词原文及嵌套回复评论

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | number | ✓ | 文档ID |

#### `addComment` - 添加评论
为iWiki文档添加评论，支持添加顶级评论或回复其他评论

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| docid | number | ✓ | 文档ID |
| content | string | ✓ | 评论内容（Markdown格式） |
| parent_id | number | | 父评论ID，回复评论时填写，默认 0 |

---

### 多维表格（Smartsheet）

#### `smartsheetGetFields` - 获取表格字段列表
获取多维表格的所有字段/列信息，返回字段列表包含字段ID、名称、类型、属性等

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | string | ✓ | 多维表格文档ID |
| viewId | string | | 视图ID，指定获取特定视图下的字段信息 |

#### `smartsheetAddField` - 添加表格字段
为多维表格添加新字段/列，支持多种字段类型

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | number | ✓ | 多维表格文档ID |
| name | string | ✓ | 字段名称 |
| type | string | ✓ | 字段类型（见下方类型列表） |
| property | object | | 字段属性配置对象 |

**支持的字段类型**：SingleText, Text, SingleSelect, MultiSelect, Number, Currency, Percent, DateTime, Attachment, Member, Checkbox, Rating, URL, Phone, Email, WorkDoc, OneWayLink, TwoWayLink, MagicLookUp, Formula, AutoNumber, CreatedTime, LastModifiedTime, CreatedBy, LastModifiedBy, Button

#### `smartsheetDeleteField` - 删除表格字段

⚠️ **安全提示**: 删除后该字段下的所有数据将被清除，操作不可恢复

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | string | ✓ | 多维表格文档ID |
| fieldId | string | ✓ | 要删除的字段ID |

#### `smartsheetGetViews` - 获取表格视图列表
获取多维表格的所有视图列表，返回视图ID、名称、类型等信息

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | string | ✓ | 多维表格文档ID |
| view_type | string | | 视图类型筛选：Grid/Gallery/Kanban/Gantt/Calendar/Architecture |

#### `smartsheetGetRecords` - 获取表格记录
获取多维表格的记录数据，支持分页、排序、筛选等多种查询方式

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | number | ✓ | 多维表格文档ID |
| pageNum | number | | 页码，默认 1 |
| pageSize | number | | 每页记录数，默认 100，最大 200 |
| viewId | string | | 视图ID |
| maxRecords | number | | 最大返回记录数 |
| sort | string | | 排序规则 |
| recordIds | string | | 记录ID列表，用逗号分隔 |
| fields | string | | 字段列表，用逗号分隔，只返回指定字段 |
| filterByFormula | string | | 筛选公式 |
| cellFormat | string | | 单元格格式 |
| fieldKey | string | | 字段键类型（id 或 name） |

#### `smartsheetAddRecords` - 批量添加记录
向多维表格批量添加新记录/行

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | number | ✓ | 多维表格文档ID |
| fieldKey | string | ✓ | 字段键类型：id 或 name |
| records | array | ✓ | 记录数组，每个记录包含 fields 对象 |
| viewId | string | | 视图ID |

**records 格式示例**：
```json
[
  {"fields": {"字段名1": "值1", "字段名2": "值2"}},
  {"fields": {"字段名1": "值3", "字段名2": "值4"}}
]
```

#### `smartsheetUpdateRecords` - 批量更新记录
根据记录ID更新指定记录的字段值，支持部分字段更新

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | number | ✓ | 多维表格文档ID |
| fieldKey | string | ✓ | 字段键类型：id 或 name |
| records | array | ✓ | 记录数组，每个记录需包含 recordId 和 fields |
| viewId | string | | 视图ID |

**records 格式示例**：
```json
[
  {"recordId": "rec123", "fields": {"字段名1": "新值1"}},
  {"recordId": "rec456", "fields": {"字段名2": "新值2"}}
]
```

#### `smartsheetDeleteRecords` - 批量删除记录

⚠️ **安全提示**: 删除操作不可恢复

| 参数 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| doc_id | string | ✓ | 多维表格文档ID |
| record_ids | array | ✓ | 要删除的记录ID数组 |

---

## 注意事项

1. 部分工具参数 `docid` 需要 number 类型，部分需要 string 类型，请注意区分
2. 创建/修改文档前建议先获取当前内容
3. 多维表格操作需要文档类型为 VIKA
4. 删除操作（标签、字段、记录）均不可恢复，请谨慎使用
5. **JSON 参数跨平台写法**：
   - **macOS / Linux**：`--args "{\"key\": \"value\"}"`
   - **Windows PowerShell**：`--args '{\"key\": \"value\"}'`（单引号包裹，内部双引号仍需 `\` 转义）
   - **Windows PowerShell 禁止**用双引号包裹 JSON（如 `--args "{"key":"value"}"`），否则内部双引号会被 PowerShell 自身吞掉，导致报错 `Unable to parse --args: Expected property name or '}' in JSON at position 1`
   - 详见 `mcporter-internal` skill 文档