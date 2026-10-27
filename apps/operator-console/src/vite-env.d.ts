/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_DEV_JWT_SECRET?: string;
  readonly VITE_SALE_API_BASE?: string;
  readonly VITE_ADMISSION_API_BASE?: string;
  readonly VITE_ORDER_API_BASE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
