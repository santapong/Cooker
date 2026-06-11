export interface APIToken {
  id: string;
  name: string;
  displayPrefix: string;
  role: string;
  createdBySub: string;
  createdByEmail: string;
  createdAt: string;
  expiresAt?: string;
  lastUsedAt?: string;
}
