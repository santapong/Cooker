import { post } from './client';

export const feedbackApi = {
  submit: (body: { category: 'bug' | 'idea' | 'other'; message: string; pageUrl: string }) =>
    post<{ issueUrl: string; issueNumber: number }>('/feedback', body),
};
