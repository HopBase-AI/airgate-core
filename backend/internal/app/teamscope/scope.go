// Package teamscope 定义团队（企业组织）管理操作的可见范围：
// 企业主 / 管理员 = 全企业；部门负责人 = 自己负责的那一个部门。
//
// 范围由请求层解析（server/middleware.RequireTeamScope），再作为**显式参数**传进
// app/member 与 app/department 的每个方法——塞进 context 传递会让"某个方法忘了取"
// 退化成静默越权，显式入参则编译期就逼着每个调用点表态。
//
// 本包只放纯数据与判定，不依赖 gin / ent / http。
package teamscope

// Scope 一次团队管理操作的范围。
type Scope struct {
	// OwnerID 数据归属的企业主 user id。部门负责人操作时这里必须是**部门所属企业主**，
	// 而不是负责人自己的 user id：service 全部按 ownerID 限定归属，填错会静默读写另一个租户。
	OwnerID int
	// DepartmentID 非 nil 即部门负责人范围：只能读写该部门本身及其成员。
	// nil = 企业主 / 管理员，全企业无限制。
	DepartmentID *int
	// ActorMemberID 部门负责人自己的成员 id（0 = 非负责人）：用于禁止改 / 删自己那条成员记录
	// ——否则负责人可自行抬额度、或把自己停用锁死。
	ActorMemberID int
}

// Owner 企业主 / 管理员范围：全企业，无部门限制。
func Owner(ownerID int) Scope { return Scope{OwnerID: ownerID} }

// DepartmentManager 部门负责人范围：ownerID 必须是该部门所属企业主。
func DepartmentManager(ownerID, departmentID, actorMemberID int) Scope {
	id := departmentID
	return Scope{OwnerID: ownerID, DepartmentID: &id, ActorMemberID: actorMemberID}
}

// IsDepartmentManager 是否部门负责人范围。
func (s Scope) IsDepartmentManager() bool { return s.DepartmentID != nil }

// ScopedDepartmentID 负责的部门 id；企业主范围返回 0。
func (s Scope) ScopedDepartmentID() int {
	if s.DepartmentID == nil {
		return 0
	}
	return *s.DepartmentID
}

// AllowsDepartment 该部门是否在范围内。企业主范围恒 true；
// 负责人范围只认自己那个部门，未分配部门（0）一律不认。
func (s Scope) AllowsDepartment(departmentID int) bool {
	if s.DepartmentID == nil {
		return true
	}
	return departmentID > 0 && departmentID == *s.DepartmentID
}

// IsSelfMember 是否部门负责人自己那条成员记录。
func (s Scope) IsSelfMember(memberID int) bool {
	return s.ActorMemberID > 0 && memberID == s.ActorMemberID
}
