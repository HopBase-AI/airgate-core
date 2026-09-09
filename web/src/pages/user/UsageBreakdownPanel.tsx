import { useTranslation } from 'react-i18next';
import { Button, Tabs } from '@heroui/react';
import { Building2, KeyRound, Layers, UsersRound } from 'lucide-react';
import { CompactDataTable, type CompactDataTableColumn } from '../../shared/components/CompactDataTable';
import { CostValue } from '../../shared/components/CostValue';
import { fmtNum } from '../../shared/columns/usageColumns';
import type { APIKeyStats, DepartmentStats, GroupStats, MemberStats, UsageStatsResp } from '../../shared/types';

export type BreakdownDimension = 'department' | 'member' | 'key' | 'group';

interface BreakdownRow {
  id: number;
  name: string;
  requests: number;
  tokens: number;
  actualCost: number;
  /** 下钻：点击后把该维度写进筛选 */
  drill?: () => void;
}

// 企业主概览的分层下钻：企业 → 部门 → 成员 → 密钥 → 分组。每张表按真实成本倒序，
// 点一行即把它写进筛选条（部门/成员/密钥），明细表与汇总随之收敛——「筛选、汇总、明细下钻」三件事一处完成。
// 「未分配」必须单列：企业主自己名下不挂部门的消耗单独一行，否则各部门加总 ≠ 企业总额。
export function UsageBreakdownPanel({
  stats,
  dimension,
  onDimensionChange,
  hasDepartments,
  hasMembers,
  totalActualCost,
  onDrill,
}: {
  stats?: UsageStatsResp;
  dimension: BreakdownDimension;
  onDimensionChange: (next: BreakdownDimension) => void;
  hasDepartments: boolean;
  hasMembers: boolean;
  totalActualCost: number;
  onDrill: (filter: { department_id?: number; member_id?: number; api_key_id?: number; group_id?: number }) => void;
}) {
  const { t } = useTranslation();
  const allTabs: Array<{ key: BreakdownDimension; label: string; icon: typeof Layers; visible: boolean }> = [
    { key: 'department', label: t('usage.breakdown_department'), icon: Building2, visible: hasDepartments },
    { key: 'member', label: t('usage.breakdown_member'), icon: UsersRound, visible: hasMembers },
    { key: 'key', label: t('usage.breakdown_key'), icon: KeyRound, visible: true },
    { key: 'group', label: t('usage.breakdown_group'), icon: Layers, visible: true },
  ];
  const tabs = allTabs.filter((tab) => tab.visible);

  const rows: BreakdownRow[] = (() => {
    switch (dimension) {
      case 'department':
        return (stats?.by_department ?? []).map((d: DepartmentStats) => ({
          id: d.department_id,
          name: d.department_id === 0 ? t('usage.unassigned') : d.name || t('team.department_deleted'),
          requests: d.requests,
          tokens: d.tokens,
          actualCost: d.actual_cost,
          drill: () => onDrill({ department_id: d.department_id }),
        }));
      case 'member':
        return (stats?.by_member ?? []).map((m: MemberStats) => ({
          id: m.member_id,
          name: m.member_id === 0 ? t('usage.owner_self') : m.name || t('usage.member_deleted'),
          requests: m.requests,
          tokens: m.tokens,
          actualCost: m.actual_cost,
          drill: () => onDrill({ member_id: m.member_id }),
        }));
      case 'key':
        return (stats?.by_key ?? []).map((k: APIKeyStats) => ({
          id: k.api_key_id,
          name: k.api_key_id === 0 ? t('usage.no_key_source') : k.name || t('usage.api_key_deleted'),
          requests: k.requests,
          tokens: k.tokens,
          actualCost: k.actual_cost,
          drill: k.api_key_id > 0 ? () => onDrill({ api_key_id: k.api_key_id }) : undefined,
        }));
      case 'group':
      default:
        return (stats?.by_group ?? []).map((g: GroupStats) => ({
          id: g.group_id,
          name: g.name || `#${g.group_id}`,
          requests: g.requests,
          tokens: g.tokens,
          actualCost: g.actual_cost,
          drill: () => onDrill({ group_id: g.group_id }),
        }));
    }
  })();

  const columns: CompactDataTableColumn<BreakdownRow>[] = [
    {
      key: 'name',
      title: tabs.find((tab) => tab.key === dimension)?.label ?? '',
      render: (row) => (
        row.drill ? (
          <Button className="max-w-full justify-start px-0 text-left" size="sm" variant="ghost" onPress={row.drill}>
            <span className="truncate">{row.name}</span>
          </Button>
        ) : (
          <span className="truncate text-text-secondary">{row.name}</span>
        )
      ),
    },
    { key: 'requests', title: t('usage.total_requests'), align: 'end', width: '7rem', render: (row) => row.requests.toLocaleString() },
    { key: 'tokens', title: t('usage.total_tokens'), align: 'end', width: '7rem', render: (row) => fmtNum(row.tokens) },
    {
      key: 'cost',
      title: t('usage.actual_cost'),
      align: 'end',
      width: '8rem',
      render: (row) => <CostValue value={row.actualCost} decimals={4} tone="actual" />,
    },
    {
      key: 'share',
      title: t('usage.share'),
      align: 'end',
      width: '5rem',
      render: (row) => (totalActualCost > 0 ? `${((row.actualCost / totalActualCost) * 100).toFixed(1)}%` : '—'),
    },
  ];

  return (
    <div className="ag-usage-breakdown mb-5">
      {/* Tabs 只做维度切换，表格放在 Tabs 之外（HeroUI Tabs 内嵌其它集合组件会崩） */}
      <div className="ag-page-toolbar mb-3">
        <Tabs selectedKey={dimension} onSelectionChange={(key) => onDimensionChange(key as BreakdownDimension)}>
          <Tabs.ListContainer className="ag-page-tabs w-full sm:w-auto">
            <Tabs.List>
              {tabs.map((tab, index) => {
                const Icon = tab.icon;
                return (
                  <Tabs.Tab key={tab.key} id={tab.key}>
                    {index > 0 ? <Tabs.Separator /> : null}
                    <Tabs.Indicator />
                    <Icon className="h-4 w-4" />
                    {tab.label}
                  </Tabs.Tab>
                );
              })}
            </Tabs.List>
          </Tabs.ListContainer>
        </Tabs>
        <span className="text-xs text-text-tertiary sm:ml-auto">{t('usage.breakdown_hint')}</span>
      </div>
      <CompactDataTable
        ariaLabel={tabs.find((tab) => tab.key === dimension)?.label ?? ''}
        columns={columns}
        emptyText={t('common.no_data')}
        rowKey={(row) => `${dimension}-${row.id}`}
        rows={rows}
      />
    </div>
  );
}
