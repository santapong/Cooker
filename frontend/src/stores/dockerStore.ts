import { create } from 'zustand';
import type { ImageInfo, ContainerInfo } from '../types/docker';
import { dockerApi } from '../api/docker';

interface DockerStore {
  images: ImageInfo[];
  containers: ContainerInfo[];
  loading: boolean;
  error: string | null;

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
    // The Docker host transport is intentionally not wired in this build
    // (honest-501 by design / P9.4), so buildImage will reject. We don't
    // track a "transport unconfigured" flag here: DockerPage shows its
    // banner unconditionally, so there's nothing to gate. Let the error
    // propagate to the caller, which surfaces a toast.
    const result = await dockerApi.buildImage(config);
    return result.buildId;
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
