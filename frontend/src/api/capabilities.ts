import { get } from './client';

export interface Capabilities {
  aiTriage: boolean;
  cloudInventory: boolean;
  feedback: boolean;
}

export const capabilitiesApi = {
  get: () => get<Capabilities>('/capabilities'),
};
