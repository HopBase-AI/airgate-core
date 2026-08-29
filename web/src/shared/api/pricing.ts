import { get } from './client';

export interface PricingOverride {
  user_id: number;
  email: string;
  username: string;
  rate: number;
  pricing_mode: string;
}

export interface PricingGroup {
  id: number;
  name: string;
  platform: string;
  rate_multiplier: number;
  is_exclusive: boolean;
  delisted: boolean;
  model_count: number;
  cost_multiplier: number;
  cost_account_id: number;
  cost_account_name: string;
  routed_accounts: number;
  overrides?: PricingOverride[];
}

export interface PricingOverview {
  groups: PricingGroup[];
}

export const pricingApi = {
  overview: () => get<PricingOverview>('/api/v1/admin/pricing/overview'),
};
