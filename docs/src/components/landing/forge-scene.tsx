"use client";

import { Canvas, useFrame } from "@react-three/fiber";
import { useMemo, useRef } from "react";
import * as THREE from "three";

const SPARK_COUNT = 40;
const STRIKE_PERIOD = 1.9;
const STRIKE_PHASE = 0.56;

type Vec3Like = {
  x: number;
  y: number;
  z: number;
  copy: (vector: Vec3Like) => Vec3Like;
  set: (x: number, y: number, z: number) => Vec3Like;
  addScaledVector: (vector: Vec3Like, scale: number) => Vec3Like;
  multiplyScalar: (scale: number) => Vec3Like;
};

type GroupRef = {
  position: {
    set: (x: number, y: number, z: number) => void;
  };
  rotation: {
    set: (x: number, y: number, z: number) => void;
  };
};

type EmissiveMaterialRef = {
  emissiveIntensity: number;
};

type MeshRef = {
  visible: boolean;
  position: {
    copy: (vector: Vec3Like) => void;
  };
  scale: {
    set: (x: number, y: number, z: number) => void;
    setScalar: (value: number) => void;
  };
};

type BasicMaterialRef = {
  opacity: number;
};

type SparkState = {
  active: boolean;
  age: number;
  life: number;
  position: Vec3Like;
  velocity: Vec3Like;
};

const IMPACT_POINT = new THREE.Vector3(0, 0.24, 0) as unknown as Vec3Like;

function easeInOutCubic(value: number) {
  if (value < 0.5) {
    return 4 * value * value * value;
  }

  return 1 - (-2 * value + 2) ** 3 / 2;
}

function easeOutCubic(value: number) {
  return 1 - (1 - value) ** 3;
}

function SteelClashRig() {
  const leftBladeRef = useRef<GroupRef | null>(null);
  const rightBladeRef = useRef<GroupRef | null>(null);

  const leftBladeMaterialRef = useRef<EmissiveMaterialRef | null>(null);
  const rightBladeMaterialRef = useRef<EmissiveMaterialRef | null>(null);

  const clashRingRef = useRef<MeshRef | null>(null);
  const clashRingMaterialRef = useRef<BasicMaterialRef | null>(null);
  const clashCoreRef = useRef<MeshRef | null>(null);
  const clashCoreMaterialRef = useRef<BasicMaterialRef | null>(null);

  const sparkMeshRefs = useRef<Array<MeshRef | null>>([]);
  const sparkMaterialRefs = useRef<Array<BasicMaterialRef | null>>([]);
  const previousPhaseRef = useRef(0);
  const lastStrikeTimeRef = useRef(-10);

  const sparkIndices = useMemo(() => {
    return Array.from({ length: SPARK_COUNT }, (_, index) => index);
  }, []);

  const sparks = useMemo<SparkState[]>(() => {
    return Array.from({ length: SPARK_COUNT }, () => ({
      active: false,
      age: 0,
      life: 0.25,
      position: new THREE.Vector3() as unknown as Vec3Like,
      velocity: new THREE.Vector3() as unknown as Vec3Like,
    }));
  }, []);

  const triggerStrike = (now: number) => {
    lastStrikeTimeRef.current = now;

    for (const spark of sparks) {
      spark.active = true;
      spark.age = 0;
      spark.life = 0.24 + Math.random() * 0.24;
      spark.position.copy(IMPACT_POINT);

      const spread = (Math.random() - 0.5) * Math.PI * 0.92;
      const speed = 3 + Math.random() * 3.8;
      const lift = 2.6 + Math.random() * 3.4;
      const depth = (Math.random() - 0.5) * 3.2;

      spark.velocity.set(Math.cos(spread) * speed, lift, depth);
    }
  };

  useFrame((state, delta) => {
    const elapsed = state.clock.getElapsedTime();
    const phase = (elapsed % STRIKE_PERIOD) / STRIKE_PERIOD;
    const previousPhase = previousPhaseRef.current;

    const crossedStrike = previousPhase < STRIKE_PHASE && phase >= STRIKE_PHASE;
    const wrappedStrike = previousPhase > phase && phase >= STRIKE_PHASE;

    if (crossedStrike || wrappedStrike) {
      triggerStrike(elapsed);
    }

    previousPhaseRef.current = phase;

    const approach =
      phase < STRIKE_PHASE
        ? easeInOutCubic(phase / STRIKE_PHASE)
        : 1 - easeOutCubic((phase - STRIKE_PHASE) / (1 - STRIKE_PHASE));
    const wobble = Math.sin(elapsed * 1.8) * 0.03;

    if (leftBladeRef.current) {
      leftBladeRef.current.position.set(
        THREE.MathUtils.lerp(-2.05, -0.42, approach),
        THREE.MathUtils.lerp(0.46, 0.28, approach),
        0.06,
      );
      leftBladeRef.current.rotation.set(
        THREE.MathUtils.lerp(-0.12, -0.04, approach) + wobble,
        0.44,
        THREE.MathUtils.lerp(0.74, 0.18, approach),
      );
    }

    if (rightBladeRef.current) {
      rightBladeRef.current.position.set(
        THREE.MathUtils.lerp(2.05, 0.42, approach),
        THREE.MathUtils.lerp(0.46, 0.28, approach),
        -0.06,
      );
      rightBladeRef.current.rotation.set(
        THREE.MathUtils.lerp(-0.12, -0.04, approach) - wobble,
        -0.44,
        THREE.MathUtils.lerp(-0.74, -0.18, approach),
      );
    }

    const strikeAge = elapsed - lastStrikeTimeRef.current;
    const strikeFlash = strikeAge >= 0 && strikeAge <= 0.32;

    if (leftBladeMaterialRef.current) {
      const glow = strikeFlash ? 0.55 - strikeAge * 1.1 : 0.18;
      leftBladeMaterialRef.current.emissiveIntensity = Math.max(0.18, glow);
    }

    if (rightBladeMaterialRef.current) {
      const glow = strikeFlash ? 0.55 - strikeAge * 1.1 : 0.18;
      rightBladeMaterialRef.current.emissiveIntensity = Math.max(0.18, glow);
    }

    if (
      clashRingRef.current &&
      clashRingMaterialRef.current &&
      clashCoreRef.current &&
      clashCoreMaterialRef.current
    ) {
      clashRingRef.current.visible = strikeFlash;
      clashCoreRef.current.visible = strikeFlash;

      if (strikeFlash) {
        const flashProgress = strikeAge / 0.32;
        const ringScale = 0.55 + flashProgress * 1.8;
        const coreScale = 0.36 + flashProgress * 0.9;

        clashRingRef.current.scale.set(ringScale, ringScale, ringScale);
        clashCoreRef.current.scale.set(coreScale, coreScale, coreScale);
        clashRingMaterialRef.current.opacity = Math.max(
          0,
          0.72 - flashProgress,
        );
        clashCoreMaterialRef.current.opacity = Math.max(
          0,
          0.88 - flashProgress * 1.3,
        );
      }
    }

    sparks.forEach((spark, index) => {
      const mesh = sparkMeshRefs.current[index];
      const material = sparkMaterialRefs.current[index];

      if (!mesh || !material) {
        return;
      }

      if (!spark.active) {
        mesh.visible = false;
        return;
      }

      spark.age += delta;

      if (spark.age >= spark.life) {
        spark.active = false;
        mesh.visible = false;
        return;
      }

      spark.velocity.y -= delta * 9.8;
      spark.velocity.multiplyScalar(Math.max(0, 1 - delta * 0.95));
      spark.position.addScaledVector(spark.velocity, delta);

      const lifeProgress = spark.age / spark.life;
      const scale = THREE.MathUtils.clamp(
        (1 - lifeProgress) * 0.11,
        0.01,
        0.11,
      );

      mesh.visible = true;
      mesh.position.copy(spark.position);
      mesh.scale.setScalar(scale);
      material.opacity = Math.max(0, 1 - lifeProgress * 1.2);
    });
  });

  return (
    <group>
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, -1.15, 0]}>
        <planeGeometry args={[16, 10]} />
        <meshStandardMaterial color="#030712" roughness={1} metalness={0.05} />
      </mesh>

      <mesh position={[0, -0.82, 0]} rotation={[0, 0, 0]}>
        <cylinderGeometry args={[0.94, 1.16, 0.62, 30]} />
        <meshStandardMaterial
          color="#334155"
          roughness={0.68}
          metalness={0.7}
        />
      </mesh>
      <mesh position={[0, -0.46, 0]}>
        <boxGeometry args={[1.72, 0.2, 0.66]} />
        <meshStandardMaterial
          color="#64748b"
          roughness={0.55}
          metalness={0.74}
        />
      </mesh>

      <group
        ref={leftBladeRef}
        position={[-2.05, 0.46, 0.06]}
        rotation={[-0.12, 0.44, 0.74]}
      >
        <mesh position={[0, -0.75, 0]}>
          <boxGeometry args={[0.16, 2.18, 0.08]} />
          <meshStandardMaterial
            ref={leftBladeMaterialRef}
            color="#cbd5e1"
            metalness={0.95}
            roughness={0.2}
            emissive="#f59e0b"
            emissiveIntensity={0.18}
          />
        </mesh>
        <mesh position={[0, 0.44, 0]} rotation={[0, 0, Math.PI]}>
          <coneGeometry args={[0.08, 0.34, 14]} />
          <meshStandardMaterial
            color="#cbd5e1"
            metalness={0.9}
            roughness={0.23}
          />
        </mesh>
        <mesh position={[0, -1.92, 0]} rotation={[Math.PI / 2, 0, 0]}>
          <torusGeometry args={[0.13, 0.03, 10, 28]} />
          <meshStandardMaterial
            color="#0f172a"
            metalness={0.68}
            roughness={0.4}
          />
        </mesh>
        <mesh position={[0, -2.18, 0]}>
          <cylinderGeometry args={[0.05, 0.05, 0.5, 14]} />
          <meshStandardMaterial
            color="#7c3aed"
            roughness={0.6}
            metalness={0.25}
          />
        </mesh>
      </group>

      <group
        ref={rightBladeRef}
        position={[2.05, 0.46, -0.06]}
        rotation={[-0.12, -0.44, -0.74]}
      >
        <mesh position={[0, -0.75, 0]}>
          <boxGeometry args={[0.16, 2.18, 0.08]} />
          <meshStandardMaterial
            ref={rightBladeMaterialRef}
            color="#e2e8f0"
            metalness={0.95}
            roughness={0.19}
            emissive="#f59e0b"
            emissiveIntensity={0.18}
          />
        </mesh>
        <mesh position={[0, 0.44, 0]} rotation={[0, 0, Math.PI]}>
          <coneGeometry args={[0.08, 0.34, 14]} />
          <meshStandardMaterial
            color="#e2e8f0"
            metalness={0.9}
            roughness={0.22}
          />
        </mesh>
        <mesh position={[0, -1.92, 0]} rotation={[Math.PI / 2, 0, 0]}>
          <torusGeometry args={[0.13, 0.03, 10, 28]} />
          <meshStandardMaterial
            color="#0f172a"
            metalness={0.68}
            roughness={0.4}
          />
        </mesh>
        <mesh position={[0, -2.18, 0]}>
          <cylinderGeometry args={[0.05, 0.05, 0.5, 14]} />
          <meshStandardMaterial
            color="#2563eb"
            roughness={0.62}
            metalness={0.25}
          />
        </mesh>
      </group>

      <mesh
        ref={clashRingRef}
        position={[IMPACT_POINT.x, IMPACT_POINT.y, IMPACT_POINT.z]}
        rotation={[Math.PI / 2, 0, 0]}
        visible={false}
      >
        <ringGeometry args={[0.08, 0.22, 32]} />
        <meshBasicMaterial
          ref={clashRingMaterialRef}
          color="#fbbf24"
          transparent
          opacity={0}
          depthWrite={false}
        />
      </mesh>

      <mesh
        ref={clashCoreRef}
        position={[IMPACT_POINT.x, IMPACT_POINT.y, IMPACT_POINT.z]}
        visible={false}
      >
        <sphereGeometry args={[0.1, 16, 16]} />
        <meshBasicMaterial
          ref={clashCoreMaterialRef}
          color="#fde68a"
          transparent
          opacity={0}
          depthWrite={false}
        />
      </mesh>

      {sparkIndices.map((index) => (
        <mesh
          key={`steel-spark-${index}`}
          ref={(element: unknown) => {
            sparkMeshRefs.current[index] = element as MeshRef | null;
          }}
          visible={false}
        >
          <sphereGeometry args={[0.055, 8, 8]} />
          <meshBasicMaterial
            ref={(element: unknown) => {
              sparkMaterialRefs.current[index] =
                element as BasicMaterialRef | null;
            }}
            color="#f59e0b"
            transparent
            opacity={0}
            depthWrite={false}
          />
        </mesh>
      ))}
    </group>
  );
}

export function ForgeScene() {
  return (
    <div className="absolute inset-0 pointer-events-none">
      <Canvas
        dpr={[1, 1.8]}
        gl={{
          antialias: true,
          alpha: true,
          powerPreference: "high-performance",
        }}
        camera={{ position: [0, 0.35, 6.7], fov: 34, near: 0.1, far: 40 }}
        onCreated={({ camera }) => {
          camera.lookAt(0, -0.1, 0);
        }}
      >
        <color attach="background" args={["#070b12"]} />
        <fog attach="fog" args={["#070b12", 8, 22]} />

        <ambientLight intensity={0.54} />
        <hemisphereLight
          color="#dbeafe"
          groundColor="#0b1120"
          intensity={0.42}
        />
        <directionalLight
          position={[-3.6, 3.8, 3.2]}
          intensity={1.08}
          color="#f8fafc"
        />
        <pointLight
          position={[0, 0.35, 0.35]}
          intensity={4.6}
          distance={7}
          color="#fb923c"
        />
        <pointLight
          position={[0, -0.5, 1.7]}
          intensity={1.4}
          distance={8}
          color="#60a5fa"
        />

        <SteelClashRig />
      </Canvas>
    </div>
  );
}
