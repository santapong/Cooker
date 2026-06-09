import { get } from './client';

export interface Capabilities {
  aiTriage: boolean;
}

export const capabilitiesApi = {
  get: () => get<Capabilities>('/capabilities'),
};
