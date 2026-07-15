from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

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

class BruttoRange(_message.Message):
    __slots__ = ("min", "max")
    MIN_FIELD_NUMBER: _ClassVar[int]
    MAX_FIELD_NUMBER: _ClassVar[int]
    min: int
    max: int
    def __init__(self, min: _Optional[int] = ..., max: _Optional[int] = ...) -> None: ...

class MassListRequest(_message.Message):
    __slots__ = ("spectra_name", "low_percentile", "high_percentile", "rel_error", "charge_max", "brutto_dict", "protocole", "spectra_path", "width", "height", "dpi", "format", "options")
    class BruttoDictEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: BruttoRange
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[BruttoRange, _Mapping]] = ...) -> None: ...
    class OptionsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SPECTRA_NAME_FIELD_NUMBER: _ClassVar[int]
    LOW_PERCENTILE_FIELD_NUMBER: _ClassVar[int]
    HIGH_PERCENTILE_FIELD_NUMBER: _ClassVar[int]
    REL_ERROR_FIELD_NUMBER: _ClassVar[int]
    CHARGE_MAX_FIELD_NUMBER: _ClassVar[int]
    BRUTTO_DICT_FIELD_NUMBER: _ClassVar[int]
    PROTOCOLE_FIELD_NUMBER: _ClassVar[int]
    SPECTRA_PATH_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    DPI_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    spectra_name: str
    low_percentile: float
    high_percentile: float
    rel_error: float
    charge_max: int
    brutto_dict: _containers.MessageMap[str, BruttoRange]
    protocole: str
    spectra_path: str
    width: int
    height: int
    dpi: int
    format: str
    options: _containers.ScalarMap[str, str]
    def __init__(self, spectra_name: _Optional[str] = ..., low_percentile: _Optional[float] = ..., high_percentile: _Optional[float] = ..., rel_error: _Optional[float] = ..., charge_max: _Optional[int] = ..., brutto_dict: _Optional[_Mapping[str, BruttoRange]] = ..., protocole: _Optional[str] = ..., spectra_path: _Optional[str] = ..., width: _Optional[int] = ..., height: _Optional[int] = ..., dpi: _Optional[int] = ..., format: _Optional[str] = ..., options: _Optional[_Mapping[str, str]] = ...) -> None: ...

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
