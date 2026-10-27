/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_DEV_JWT_SECRET?: string;
  /** Google OAuth 2.0 Web client ID for Gmail one-click sign-up (optional). */
  readonly VITE_GOOGLE_CLIENT_ID?: string;
  readonly VITE_API_SALE?: string;
  readonly VITE_API_ADMISSION?: string;
  readonly VITE_API_INVENTORY?: string;
  readonly VITE_API_ORDER?: string;
  readonly VITE_SALE_API_BASE?: string;
  readonly VITE_ADMISSION_API_BASE?: string;
  readonly VITE_ORDER_API_BASE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
