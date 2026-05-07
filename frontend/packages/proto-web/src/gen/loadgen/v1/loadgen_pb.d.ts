import type { GenEnum, GenFile, GenMessage, GenService } from "@bufbuild/protobuf/codegenv1";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";
/**
 * Describes the file loadgen/v1/loadgen.proto.
 */
export declare const file_loadgen_v1_loadgen: GenFile;
/**
 * @generated from message loadgen.v1.LoadTestRequest
 */
export type LoadTestRequest = Message<"loadgen.v1.LoadTestRequest"> & {
    /**
     * When set to a known preset, scenario/target_rps/duration_seconds are derived
     * server-side. When CUSTOM (or PRESET_UNSPECIFIED), the explicit fields below apply.
     *
     * @generated from field: loadgen.v1.Preset preset = 1;
     */
    preset: Preset;
    /**
     * "read", "write" or "mixed". Required when preset is CUSTOM/UNSPECIFIED.
     *
     * @generated from field: string scenario = 2;
     */
    scenario: string;
    /**
     * @generated from field: uint32 target_rps = 3;
     */
    targetRps: number;
    /**
     * @generated from field: uint32 duration_seconds = 4;
     */
    durationSeconds: number;
};
/**
 * Describes the message loadgen.v1.LoadTestRequest.
 * Use `create(LoadTestRequestSchema)` to create a new message.
 */
export declare const LoadTestRequestSchema: GenMessage<LoadTestRequest>;
/**
 * @generated from message loadgen.v1.LoadTestSample
 */
export type LoadTestSample = Message<"loadgen.v1.LoadTestSample"> & {
    /**
     * @generated from field: google.protobuf.Timestamp ts = 1;
     */
    ts?: Timestamp;
    /**
     * @generated from field: uint32 elapsed_seconds = 2;
     */
    elapsedSeconds: number;
    /**
     * @generated from field: double current_rps = 3;
     */
    currentRps: number;
    /**
     * @generated from field: uint64 total_requests = 4;
     */
    totalRequests: bigint;
    /**
     * @generated from field: uint64 errors = 5;
     */
    errors: bigint;
    /**
     * @generated from field: double p50_ms = 6;
     */
    p50Ms: number;
    /**
     * @generated from field: double p95_ms = 7;
     */
    p95Ms: number;
    /**
     * @generated from field: double p99_ms = 8;
     */
    p99Ms: number;
    /**
     * @generated from field: loadgen.v1.Status status = 9;
     */
    status: Status;
    /**
     * @generated from field: string message = 10;
     */
    message: string;
};
/**
 * Describes the message loadgen.v1.LoadTestSample.
 * Use `create(LoadTestSampleSchema)` to create a new message.
 */
export declare const LoadTestSampleSchema: GenMessage<LoadTestSample>;
/**
 * @generated from enum loadgen.v1.Preset
 */
export declare enum Preset {
    /**
     * @generated from enum value: PRESET_UNSPECIFIED = 0;
     */
    PRESET_UNSPECIFIED = 0,
    /**
     * @generated from enum value: LOW = 1;
     */
    LOW = 1,
    /**
     * @generated from enum value: MEDIUM = 2;
     */
    MEDIUM = 2,
    /**
     * @generated from enum value: HIGH = 3;
     */
    HIGH = 3,
    /**
     * @generated from enum value: XHIGH = 4;
     */
    XHIGH = 4,
    /**
     * @generated from enum value: XXHIGH = 5;
     */
    XXHIGH = 5,
    /**
     * @generated from enum value: INSANE = 6;
     */
    INSANE = 6,
    /**
     * @generated from enum value: CUSTOM = 7;
     */
    CUSTOM = 7
}
/**
 * Describes the enum loadgen.v1.Preset.
 */
export declare const PresetSchema: GenEnum<Preset>;
/**
 * @generated from enum loadgen.v1.Status
 */
export declare enum Status {
    /**
     * @generated from enum value: STATUS_UNSPECIFIED = 0;
     */
    STATUS_UNSPECIFIED = 0,
    /**
     * @generated from enum value: RUNNING = 1;
     */
    RUNNING = 1,
    /**
     * @generated from enum value: COMPLETED = 2;
     */
    COMPLETED = 2,
    /**
     * @generated from enum value: FAILED = 3;
     */
    FAILED = 3
}
/**
 * Describes the enum loadgen.v1.Status.
 */
export declare const StatusSchema: GenEnum<Status>;
/**
 * LoadGenService streams synthetic load against the gateway and resolver and emits
 * per-second samples while the run is in flight.
 *
 * @generated from service loadgen.v1.LoadGenService
 */
export declare const LoadGenService: GenService<{
    /**
     * RunLoadTest starts a load run and streams samples until it terminates.
     *
     * @generated from rpc loadgen.v1.LoadGenService.RunLoadTest
     */
    runLoadTest: {
        methodKind: "server_streaming";
        input: typeof LoadTestRequestSchema;
        output: typeof LoadTestSampleSchema;
    };
}>;
