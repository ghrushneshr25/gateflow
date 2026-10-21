export type Offering = {
  id: string;
  name: string;
  description?: string;
  category: string;
  status: string;
};

export type Session = {
  id: string;
  offering_id: string;
  starts_at: string;
  ends_at: string;
  sale_opens_at: string;
  sale_closes_at: string;
  status: string;
};

export type TicketType = {
  id: string;
  session_id: string;
  name: string;
  capacity: number;
  price_cents: number;
  currency: string;
  inventory_pool_id?: string | null;
};

export type QueueEntry = {
  id: string;
  session_id: string;
  buyer_id: string;
  sequence_no: number;
  status: string;
  entitlement_id?: string | null;
};

export type Entitlement = {
  id: string;
  session_id: string;
  buyer_id: string;
  status: string;
  unbound_expires_at?: string;
};

export type Order = {
  id: string;
  buyer_id?: string;
  session_id: string;
  ticket_type_id?: string;
  pool_id?: string;
  status: string;
  quantity: number;
  amount_cents: number;
  entitlement_id?: string;
  payment_operation_id?: string;
  updated_at?: string;
};

export type ApiError = { status: number; message: string; code?: string };
