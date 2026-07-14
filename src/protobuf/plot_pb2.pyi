from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class TextRequest(_message.Message):
    __slots__ = ("text", "language", "uppercase", "tags")
    TEXT_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    UPPERCASE_FIELD_NUMBER: _ClassVar[int]
    TAGS_FIELD_NUMBER: _ClassVar[int]
    text: str
    language: str
    uppercase: bool
    tags: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, text: _Optional[str] = ..., language: _Optional[str] = ..., uppercase: _Optional[bool] = ..., tags: _Optional[_Iterable[str]] = ...) -> None: ...

class TextResponse(_message.Message):
    __slots__ = ("original_text", "processed_text", "length", "language", "metadata")
    class MetadataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ORIGINAL_TEXT_FIELD_NUMBER: _ClassVar[int]
    PROCESSED_TEXT_FIELD_NUMBER: _ClassVar[int]
    LENGTH_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    METADATA_FIELD_NUMBER: _ClassVar[int]
    original_text: str
    processed_text: str
    length: int
    language: str
    metadata: _containers.ScalarMap[str, str]
    def __init__(self, original_text: _Optional[str] = ..., processed_text: _Optional[str] = ..., length: _Optional[int] = ..., language: _Optional[str] = ..., metadata: _Optional[_Mapping[str, str]] = ...) -> None: ...

class PlotRequest(_message.Message):
    __slots__ = ("x_values", "y_values", "title", "x_label", "y_label", "plot_type", "color", "grid", "width", "height", "dpi", "format", "options")
    class OptionsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    X_VALUES_FIELD_NUMBER: _ClassVar[int]
    Y_VALUES_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    X_LABEL_FIELD_NUMBER: _ClassVar[int]
    Y_LABEL_FIELD_NUMBER: _ClassVar[int]
    PLOT_TYPE_FIELD_NUMBER: _ClassVar[int]
    COLOR_FIELD_NUMBER: _ClassVar[int]
    GRID_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    DPI_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    x_values: _containers.RepeatedScalarFieldContainer[float]
    y_values: _containers.RepeatedScalarFieldContainer[float]
    title: str
    x_label: str
    y_label: str
    plot_type: str
    color: str
    grid: bool
    width: int
    height: int
    dpi: int
    format: str
    options: _containers.ScalarMap[str, str]
    def __init__(self, x_values: _Optional[_Iterable[float]] = ..., y_values: _Optional[_Iterable[float]] = ..., title: _Optional[str] = ..., x_label: _Optional[str] = ..., y_label: _Optional[str] = ..., plot_type: _Optional[str] = ..., color: _Optional[str] = ..., grid: _Optional[bool] = ..., width: _Optional[int] = ..., height: _Optional[int] = ..., dpi: _Optional[int] = ..., format: _Optional[str] = ..., options: _Optional[_Mapping[str, str]] = ...) -> None: ...

class PlotResponse(_message.Message):
    __slots__ = ("image_data", "format", "size_bytes", "mime_type", "generated_at", "info")
    IMAGE_DATA_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    MIME_TYPE_FIELD_NUMBER: _ClassVar[int]
    GENERATED_AT_FIELD_NUMBER: _ClassVar[int]
    INFO_FIELD_NUMBER: _ClassVar[int]
    image_data: bytes
    format: str
    size_bytes: int
    mime_type: str
    generated_at: int
    info: PlotInfo
    def __init__(self, image_data: _Optional[bytes] = ..., format: _Optional[str] = ..., size_bytes: _Optional[int] = ..., mime_type: _Optional[str] = ..., generated_at: _Optional[int] = ..., info: _Optional[_Union[PlotInfo, _Mapping]] = ...) -> None: ...

class PlotChunk(_message.Message):
    __slots__ = ("chunk_data", "chunk_number", "is_last", "total_size")
    CHUNK_DATA_FIELD_NUMBER: _ClassVar[int]
    CHUNK_NUMBER_FIELD_NUMBER: _ClassVar[int]
    IS_LAST_FIELD_NUMBER: _ClassVar[int]
    TOTAL_SIZE_FIELD_NUMBER: _ClassVar[int]
    chunk_data: bytes
    chunk_number: int
    is_last: bool
    total_size: int
    def __init__(self, chunk_data: _Optional[bytes] = ..., chunk_number: _Optional[int] = ..., is_last: _Optional[bool] = ..., total_size: _Optional[int] = ...) -> None: ...

class MultiPlotRequest(_message.Message):
    __slots__ = ("plots", "layout")
    PLOTS_FIELD_NUMBER: _ClassVar[int]
    LAYOUT_FIELD_NUMBER: _ClassVar[int]
    plots: _containers.RepeatedCompositeFieldContainer[PlotRequest]
    layout: str
    def __init__(self, plots: _Optional[_Iterable[_Union[PlotRequest, _Mapping]]] = ..., layout: _Optional[str] = ...) -> None: ...

class PlotInfo(_message.Message):
    __slots__ = ("title", "width", "height", "dpi")
    TITLE_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    DPI_FIELD_NUMBER: _ClassVar[int]
    title: str
    width: int
    height: int
    dpi: int
    def __init__(self, title: _Optional[str] = ..., width: _Optional[int] = ..., height: _Optional[int] = ..., dpi: _Optional[int] = ...) -> None: ...

class ElementRange(_message.Message):
    __slots__ = ("min", "max")
    MIN_FIELD_NUMBER: _ClassVar[int]
    MAX_FIELD_NUMBER: _ClassVar[int]
    min: int
    max: int
    def __init__(self, min: _Optional[int] = ..., max: _Optional[int] = ...) -> None: ...

class MassListRequest(_message.Message):
    __slots__ = ("spectra_name", "low_percentile", "high_percentile", "rel_error", "charge_max", "brutto_dict", "protocole")
    class BruttoDictEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: ElementRange
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[ElementRange, _Mapping]] = ...) -> None: ...
    SPECTRA_NAME_FIELD_NUMBER: _ClassVar[int]
    LOW_PERCENTILE_FIELD_NUMBER: _ClassVar[int]
    HIGH_PERCENTILE_FIELD_NUMBER: _ClassVar[int]
    REL_ERROR_FIELD_NUMBER: _ClassVar[int]
    CHARGE_MAX_FIELD_NUMBER: _ClassVar[int]
    BRUTTO_DICT_FIELD_NUMBER: _ClassVar[int]
    PROTOCOLE_FIELD_NUMBER: _ClassVar[int]
    spectra_name: str
    low_percentile: float
    high_percentile: float
    rel_error: float
    charge_max: int
    brutto_dict: _containers.MessageMap[str, ElementRange]
    protocole: str
    def __init__(self, spectra_name: _Optional[str] = ..., low_percentile: _Optional[float] = ..., high_percentile: _Optional[float] = ..., rel_error: _Optional[float] = ..., charge_max: _Optional[int] = ..., brutto_dict: _Optional[_Mapping[str, ElementRange]] = ..., protocole: _Optional[str] = ...) -> None: ...

class MassListResponse(_message.Message):
    __slots__ = ("image_data", "format", "size_bytes", "mime_type", "generated_at", "info")
    IMAGE_DATA_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    MIME_TYPE_FIELD_NUMBER: _ClassVar[int]
    GENERATED_AT_FIELD_NUMBER: _ClassVar[int]
    INFO_FIELD_NUMBER: _ClassVar[int]
    image_data: bytes
    format: str
    size_bytes: int
    mime_type: str
    generated_at: int
    info: PlotInfo
    def __init__(self, image_data: _Optional[bytes] = ..., format: _Optional[str] = ..., size_bytes: _Optional[int] = ..., mime_type: _Optional[str] = ..., generated_at: _Optional[int] = ..., info: _Optional[_Union[PlotInfo, _Mapping]] = ...) -> None: ...
