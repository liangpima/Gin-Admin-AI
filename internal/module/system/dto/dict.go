package dto

// 长度上限说明：下列 max 值与 sql/init.sql 的列宽一一对应
// （name/type/label/value/css_class/list_class 都是 varchar(128)，remark 是 varchar(500)）。
//
// 为什么要在 DTO 层拦：此前这些字段完全没有上限，超长输入会被 MySQL 以
// `Error 1406: Data too long for column` 拒绝，而它是一条**系统错误** ——
// 对外只会得到「服务器内部错误」，用户看不出是自己填太长了。
// 在入口处拦下来是 400 + 明确的字段名，既不泄漏表结构也不需要 Service 额外判断。
//
// 注意 validator 的 max 对字符串按**字符数**（rune）比较，与 MySQL varchar
// 在 utf8mb4 下的计数口径一致，因此不会出现「中文 128 字被判超长」的偏差。

// CreateDictTypeRequest 新增字典类型
type CreateDictTypeRequest struct {
	Name string `json:"name" binding:"required,max=128"`
	// Type 字典类型编码，业务代码里按它取值（如 useDict('sys_user_status')），
	// 因此只允许小写字母、数字、下划线，格式校验在 Service 层。
	Type string `json:"type" binding:"required,max=128"`
}

// UpdateDictTypeRequest 修改字典类型。
//
// 刻意不含 Type：它是字典数据的外键键名（sys_dict_data.dict_type 存的就是它），
// 允许改会让已有字典数据全部变成孤儿，界面上表现为「数据还在但一个都取不到」。
// 需要换编码时正确做法是新建类型再迁数据。
type UpdateDictTypeRequest struct {
	Name   string `json:"name" binding:"required,max=128"`
	Status *int8  `json:"status"`
	Remark string `json:"remark" binding:"max=500"`
}

// CreateDictDataRequest 新增字典数据
type CreateDictDataRequest struct {
	DictType  string `json:"dictType" binding:"required,max=128"`
	Label     string `json:"label" binding:"required,max=128"`
	Value     string `json:"value" binding:"required,max=128"`
	Sort      int    `json:"sort"`
	CssClass  string `json:"cssClass" binding:"max=128"`
	ListClass string `json:"listClass" binding:"max=128"`
	Remark    string `json:"remark" binding:"max=500"`
}

// UpdateDictDataRequest 修改字典数据。
//
// 不含 DictType：归属类型不允许改。改归属相当于把选项从一组搬到另一组，
// 原类型下会少一个选项、新类型下会多一个，语义上应「删除后新建」而不是修改。
type UpdateDictDataRequest struct {
	Label     string `json:"label" binding:"required,max=128"`
	Value     string `json:"value" binding:"required,max=128"`
	Sort      int    `json:"sort"`
	CssClass  string `json:"cssClass" binding:"max=128"`
	ListClass string `json:"listClass" binding:"max=128"`
	Status    *int8  `json:"status"`
	Remark    string `json:"remark" binding:"max=500"`
}
