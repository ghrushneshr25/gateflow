export type Order = {
  id: string;
  buyer_id: string;
  session_id: string;
  status: string;
  quantity: number;
  amount_cents: number;
  payment_operation_id?: string;
  compensation_operation_id?: string;
  updated_at?: string;
  created_at?: string;
};

export type ReconciliationList = {
  reconciliation_required: Order[];
  payment_pending: Order[];
};

export type ReconcileResponse = {
  order: Order;
  result: string;
};

export type QuarantinedEvent = {
  id: number;
  event_id: string;
  event_type: string;
  aggregate_id: string;
  attempt_count: number;
  replay_count: number;
  last_error?: string | null;
  last_replay_at?: string | null;
  last_replay_by?: string | null;
  created_at: string;
  correlation_id: string;
};

export type QuarantinedList = { items: QuarantinedEvent[] };

export type ReplayResponse = {
  event: QuarantinedEvent;
  result: string;
  note?: string;
};

export type Coordination = {
  id: string;
  epoch: number;
  mode: string;
  fence_reason?: string | null;
  fenced_at?: string | null;
  fenced_by?: string | null;
  updated_at: string;
};

export type ApiError = {
  status: number;
  message: string;
  code?: string;
};

export type Offering = {
  id: string;
  name: string;
  description?: string;
  category: string;
  status: string;
  created_at?: string;
  updated_at?: string;
};

export type Session = {
  id: string;
  offering_id: string;
  starts_at: string;
  ends_at: string;
  sale_opens_at: string;
  sale_closes_at: string;
  status: string;
  admission_strategy?: string;
};

export type TicketType = {
  id: string;
  session_id: string;
  name: string;
  section?: string;
  capacity: number;
  price_cents: number;
  currency: string;
  inventory_pool_id?: string | null;
};

export type Budget = {
  scope_key: string;
  max_grants: number;
  grants_issued: number;
  max_active_checkouts: number;
  active_checkouts: number;
  coordination_epoch: number;
};
