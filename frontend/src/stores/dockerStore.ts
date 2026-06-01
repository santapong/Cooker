import { create } from 'zustand';
import type { ImageInfo, ContainerInfo } from '../types/docker';
import { dockerApi } from '../api/docker';

interface DockerStore {
  images: ImageInfo[];
  containers: ContainerInfo[];
  loading: boolean;
  error: string | null;
  /**
   * True when the backend returned HTTP 501 — the Docker host transport is
   * not configured (COOKER_DOCKER_HOST is unset / P9.4 not wired up).
   * List endpoints still return 200 + [] so this flag is only set when an
   * inspect or mutating action is attempted.
   */
  transportUnconfigured: boolean;

  fetchImages: () => Promise<void>;
  fetchContainers: () => Promise<void>;
  buildImage: (config: { dockerfile: string; context: string; tags: string[] }) => Promise<string>;
  deleteImage: (id: string) => Promise<void>;
  stopContainer: (id: string) => Promise<void>;
  deleteContainer: (id: string) => Promise<void>;
}

export const useDockerStore = create<DockerStore>((set) => ({
  images: [],
  containers: [],
  loading: false,
  error: null,
  transportUnconfigured: false,

  fetchImages: async () => {
    set({ loading: true, error: null });
    try {
      const images = await dockerApi.listImages();
      set({ images, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  fetchContainers: async () => {
    set({ loading: true, error: null });
    try {
      const containers = await dockerApi.listContainers();
      set({ containers, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  buildImage: async (config) => {
    try {
      const result = await dockerApi.buildImage(config);
      return result.buildId;
    } catch (e) {
      const msg = (e as Error).message;
      if (msg.includes('not configured') || msg.includes('transport')) {
        set({ transportUnconfigured: true });
      }
      throw e;
    }
  },

  deleteImage: async (id: string) => {
    await dockerApi.deleteImage(id);
    set((state) => ({ images: state.images.filter((i) => i.id !== id) }));
  },

  stopContainer: async (id: string) => {
    await dockerApi.stopContainer(id);
  },

  deleteContainer: async (id: string) => {
    await dockerApi.deleteContainer(id);
    set((state) => ({ containers: state.containers.filter((c) => c.id !== id) }));
  },
}));
